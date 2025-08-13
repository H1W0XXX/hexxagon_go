# train_hex_cnn_ddp_memaug.py
import os, math, time, argparse, random
import pandas as pd
import numpy as np
import torch, torch.nn as nn, torch.nn.functional as F
import torch.distributed as dist
from torch.utils.data import Dataset, DataLoader, DistributedSampler, random_split
from torch.amp import autocast, GradScaler

GRID, RADIUS = 9, 4

def in_bounds(q, r, R=RADIUS):
    return abs(q) <= R and abs(r) <= R and abs(-q - r) <= R

def from_index(idx):
    q = (idx % GRID) - RADIUS
    r = (idx // GRID) - RADIUS
    return q, r

def build_geom_mask_81():
    m = torch.zeros(81, dtype=torch.bool)
    for idx in range(81):
        q, r = from_index(idx)
        if in_bounds(q, r):
            m[idx] = True
    return m

GEOM_MASK_81 = build_geom_mask_81()  # 常量

def rot_axial(q, r, k):
    k %= 6
    if k == 0: return  q,        r
    if k == 1: return -r,        q + r
    if k == 2: return -(q + r),  q
    if k == 3: return -q,       -r
    if k == 4: return  r,       -(q + r)
    return q + r,   -q

def mirror_axial(q, r):
    return -q - r, r

def to_index(q, r):   return (r + RADIUS) * GRID + (q + RADIUS)

def build_permutations():
    import numpy as np, torch
    perms     = np.empty((12, 81), dtype=np.int64)
    inv_perms = np.empty((12, 81), dtype=np.int64)

    # 12 个变换：6 旋转 + 6(镜像后旋转)
    Fs = []
    for k in range(6):
        Fs.append(lambda q, r, k=k: rot_axial(q, r, k))
    for k in range(6):
        Fs.append(lambda q, r, k=k: rot_axial(*mirror_axial(q, r), k))

    for a, f in enumerate(Fs):
        perm = np.arange(81, dtype=np.int64)  # 先恒等，保证安全
        for idx in range(81):
            q = (idx % GRID) - RADIUS
            r = (idx // GRID) - RADIUS
            if not in_bounds(q, r):
                continue  # 外框：保持恒等
            q2, r2 = f(q, r)
            if in_bounds(q2, r2):
                perm[idx] = (r2 + RADIUS) * GRID + (q2 + RADIUS)
            else:
                perm[idx] = idx  # 变换越界则退回恒等，绝不越界
        perms[a] = perm
        inv = np.empty(81, dtype=np.int64)
        inv[perm] = np.arange(81, dtype=np.int64)
        inv_perms[a] = inv

    # 保险检查（不依赖优化选项）
    if int(perms.min()) < 0 or int(perms.max()) >= 81:
        raise RuntimeError(f"perm out of range: [{int(perms.min())}, {int(perms.max())}]")
    return torch.from_numpy(perms), torch.from_numpy(inv_perms)

PERMS, INV_PERMS = build_permutations()

# ====== 模型 ======
class ResidualBlock(nn.Module):
    def __init__(self, ch):
        super().__init__()
        self.c1 = nn.Conv2d(ch, ch, 3, 1, 1, bias=False); self.b1 = nn.BatchNorm2d(ch)
        self.c2 = nn.Conv2d(ch, ch, 3, 1, 1, bias=False); self.b2 = nn.BatchNorm2d(ch)
    def forward(self, x):
        y = F.relu(self.b1(self.c1(x))); y = self.b2(self.c2(y))
        return F.relu(x + y)

class HexResNet(nn.Module):
    def __init__(self, ch=64, blocks=4):
        super().__init__()
        self.stem = nn.Sequential(nn.Conv2d(3, ch, 3, 1, 1, bias=False), nn.BatchNorm2d(ch), nn.ReLU(inplace=True))
        self.body = nn.Sequential(*[ResidualBlock(ch) for _ in range(blocks)])
        self.head_p = nn.Sequential(nn.Conv2d(ch, 32, 1), nn.ReLU(inplace=True),
                                    nn.Flatten(), nn.Linear(32*9*9, 81))
        self.head_v = nn.Sequential(nn.Conv2d(ch, 32, 1), nn.ReLU(inplace=True),
                                    nn.AdaptiveAvgPool2d(1), nn.Flatten(),
                                    nn.Linear(32, 1), nn.Tanh())
    def forward(self, x, mask=None):
        x = self.stem(x); x = self.body(x)
        p = self.head_p(x)
        if mask is not None:
            # 用 float32 做掩码，避免半精度下的下溢/梯度问题
            p = p.float().masked_fill(~mask, -1e9)
        v = self.head_v(x)
        return p, v

# ====== DDP 工具 ======
def ddp_setup():
    dist.init_process_group(backend="nccl")
    local_rank = int(os.environ["LOCAL_RANK"])
    torch.cuda.set_device(local_rank)
    return local_rank

def is_main():
    return (not dist.is_available()) or (not dist.is_initialized()) or dist.get_rank() == 0

def all_reduce_mean(x: torch.Tensor):
    dist.all_reduce(x, op=dist.ReduceOp.SUM)
    x /= dist.get_world_size()
    return x

# ====== 预计算 12× 增广并常驻内存 ======
def load_csv_aug12(csv_path: str, store_dtype="uint8"):
    # 读取：0..242 特征，243 move，244 z，(可选) 245 gameID
    try:
        df = pd.read_csv(csv_path, header=None, dtype="int8", engine="pyarrow")
    except Exception:
        df = pd.read_csv(csv_path, header=None, dtype="int8")

    X = torch.from_numpy(df.iloc[:, :243].values)  # int8 {0,1}
    move = torch.from_numpy(df.iloc[:, 243].values.astype(np.int64))
    z = torch.from_numpy(df.iloc[:, 244].values.astype(np.float32)).view(-1, 1)
    del df  # 释放 DataFrame

    N = X.shape[0]
    # (N,3,81)
    X = X.view(N, 3, 9, 9).reshape(N, 3, 81)

    X_augs = []
    move_augs = []

    for a in range(12):
        invp = INV_PERMS[a]  # (81,)
        Xa = X.index_select(2, invp)  # (N,3,81)
        X_augs.append(Xa)
        move_augs.append(PERMS[a][move])  # (N,)

    X_aug = torch.cat(X_augs, dim=0).reshape(-1, 3, 9, 9)   # (12N,3,9,9)
    move_aug = torch.cat(move_augs, dim=0)                  # (12N,)
    z_aug = z.repeat(12, 1)                                 # (12N,1)

    if store_dtype == "uint8":
        X_aug = X_aug.to(torch.uint8)  # 仅 0/1；取样时转 float32
    else:
        X_aug = X_aug.to(torch.float32)

    if is_main():
        gb = X_aug.numel() * X_aug.element_size() / (1024**3) \
           + move_aug.numel() * move_aug.element_size() / (1024**3) \
           + z_aug.numel() * z_aug.element_size() / (1024**3)
        print(f"[load_csv_aug12] base N={N} -> aug {X_aug.shape[0]} samples; RAM≈{gb:.1f} GB")

    return X_aug, move_aug.long(), z_aug.float()

class HexDatasetPrecomputed(Dataset):
    def __init__(self, X_aug, move_aug, z_aug, floatize_on_get=True):
        self.X = X_aug             # uint8 或 float32
        self.move = move_aug.long()
        self.z = z_aug.float()
        self.floatize_on_get = floatize_on_get

    def __len__(self): return self.X.shape[0]

    def __getitem__(self, i):
        x = self.X[i]
        if self.floatize_on_get and x.dtype == torch.uint8:
            x = x.float()  # 0/1 → float32
        return x, self.move[i], self.z[i]

# ====== 训练 ======
def get_mask(batch_size, device):
    # (B,81) 的几何掩码：棋盘内=True，外框=False
    return GEOM_MASK_81.to(device).unsqueeze(0).expand(batch_size, -1)

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--csv", default="dataset.csv")
    ap.add_argument("--out", default="hex_cnn.pt")
    ap.add_argument("--epochs", type=int, default=15)
    ap.add_argument("--batch", type=int, default=16384)  # 每GPU的batch
    ap.add_argument("--lr", type=float, default=1e-3)
    ap.add_argument("--wd", type=float, default=1e-4)
    ap.add_argument("--workers", type=int, default=8)
    ap.add_argument("--store-float32", action="store_true",
                    help="把增广特征直接存 float32（更大内存，少一步转换）")
    ap.add_argument("--channels", type=int, default=256)
    ap.add_argument("--blocks",   type=int, default=8)
    args = ap.parse_args()

    local_rank = ddp_setup()
    torch.backends.cudnn.benchmark = True
    random.seed(42 + local_rank); torch.manual_seed(42 + local_rank)

    store_dtype = "float32" if args.store_float32 else "uint8"
    X_aug, move_aug, z_aug = load_csv_aug12(args.csv, store_dtype=store_dtype)
    ds_full = HexDatasetPrecomputed(X_aug, move_aug, z_aug, floatize_on_get=not args.store_float32)

    # 切分 train/val（分布式下每个 rank 都做同样切分，但之后用 DistributedSampler 切 shard）
    n_train = int(len(ds_full) * 0.95)
    train_set, val_set = random_split(ds_full, [n_train, len(ds_full)-n_train], generator=torch.Generator().manual_seed(1234))

    train_sampler = DistributedSampler(train_set, shuffle=True, drop_last=False)
    val_sampler   = DistributedSampler(val_set, shuffle=False, drop_last=False)

    ld_train = DataLoader(train_set, batch_size=args.batch, sampler=train_sampler,
                          num_workers=args.workers, pin_memory=True, persistent_workers=True)
    ld_val   = DataLoader(val_set, batch_size=args.batch*2, sampler=val_sampler,
                          num_workers=max(2, args.workers//2), pin_memory=True, persistent_workers=True)

    net = HexResNet(ch=args.channels, blocks=args.blocks).to(local_rank)
    # 1. 先用 DDP 包装模型
    if dist.is_initialized():
        # args.lr *= dist.get_world_size()
        net = nn.parallel.DistributedDataParallel(net, device_ids=[local_rank], output_device=local_rank)

    # 2. 然后基于 DDP 包装后的模型创建优化器
    #    现在 opt 会正确地引用 DDP 管理的参数
    opt = torch.optim.AdamW(net.parameters(), lr=args.lr, weight_decay=args.wd)
    scaler = GradScaler()
    #net = nn.parallel.DistributedDataParallel(net, device_ids=[local_rank], output_device=local_rank)

    best = math.inf
    for epoch in range(1, args.epochs+1):
        t0 = time.time()
        step = 0
        net.train(); train_sampler.set_epoch(epoch)
        for s, move, z in ld_train:
            s = s.to(local_rank, non_blocking=True)
            move = move.to(local_rank, non_blocking=True)
            z = z.to(local_rank, non_blocking=True)
            mask = get_mask(s.size(0), local_rank)
            with autocast(device_type="cuda"):
                p, v = net(s, mask)
                loss = F.cross_entropy(p, move) + F.mse_loss(v, z)
            opt.zero_grad(set_to_none=True)
            scaler.scale(loss).backward()
            scaler.unscale_(opt)
            torch.nn.utils.clip_grad_norm_(net.parameters(), 1.0)
            before = scaler.get_scale()
            scaler.step(opt); scaler.update()
            after = scaler.get_scale()
            if is_main() and step % 50 == 0:
                print(f"[train] step={step} loss={loss.item():.4f} scale={before}->{after} lr={opt.param_groups[0]['lr']:.2e}")
            step += 1

        # 验证集 all-reduce 平均
        net.eval()
        val_sum = torch.zeros(1, device=local_rank)
        val_cnt = torch.zeros(1, device=local_rank)

        with torch.no_grad():
            for s, move, z in ld_val:
                s = s.to(local_rank, non_blocking=True)
                move = move.to(local_rank, non_blocking=True)
                z = z.to(local_rank, non_blocking=True)
                mask = get_mask(s.size(0), local_rank)

                p, v = net(s, mask)
                l = F.cross_entropy(p, move, reduction="sum") + F.mse_loss(v, z, reduction="sum")
                val_sum += l
                val_cnt += s.new_tensor(s.size(0), dtype=torch.float32)

        # 只在这里做一次全局汇总
        dist.all_reduce(val_sum, op=dist.ReduceOp.SUM)
        dist.all_reduce(val_cnt, op=dist.ReduceOp.SUM)

        val_loss = (val_sum / val_cnt).item()
        dt = time.time() - t0
        if is_main():
            print(f"Epoch {epoch}/{args.epochs}  val_loss={val_loss:.4f}  time={dt:.1f}s")

        if is_main() and val_loss < best:
            best = val_loss
            torch.save(net.module.state_dict(), args.out)
            print(f"  ↳ saved weights to {args.out}")

    # 仅 rank0 导出 TorchScript
    if is_main():
        net.module.eval()
        example = torch.randn(1,3,9,9, device=local_rank)
        f = lambda x: net.module(x, None)
        traced = torch.jit.trace(f, example)
        traced.save(os.path.splitext(args.out)[0] + ".ts")
        print("TorchScript saved!")

    if dist.is_initialized():
        dist.barrier(); dist.destroy_process_group()

if __name__ == "__main__":
    print("perm range:", int(PERMS.min()), int(PERMS.max()))
    # 还可检查可逆性
    for a in range(12):
        assert torch.equal(INV_PERMS[a][PERMS[a]], torch.arange(81))
    main()

# cp /home/qujing/gotest/dataset.csv /home/qujing/gotest/train1/dataset.csv
# torchrun --nproc_per_node=2 train_hex_cnn.py --csv dataset.csv --out hex_cnn.pt
# curl -# -F "file=@/home/qujing/gotest/train1/hex_cnn.pt" http://82.157.129.23:3888/upload
# python export_hex_cnn_to_onnx.py --pt hex_cnn.pt --onnx hex_cnn.onnx --channels 256 --blocks 8