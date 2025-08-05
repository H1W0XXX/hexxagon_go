#!/usr/bin/env python3
# train_hex_cnn.py
# ---------------------------------
# Supervised warm-up for Hexxagon CNN
# ---------------------------------

import os, math, time, argparse
import pandas as pd
import torch, torch.nn as nn, torch.nn.functional as F
from torch.utils.data import DataLoader, TensorDataset
from torch.cuda.amp import GradScaler, autocast
from tqdm import tqdm

# -----------  Hyper-params  -------------
BATCH      = 4096
EPOCHS     = 10
LR         = 2e-3
WEIGHT_DEC = 1e-4
RES_BLOCKS = 4      # 堆叠多少残差块
CHANNELS   = 64     # 每层通道数
DEVICE     = "cuda" if torch.cuda.is_available() else "cpu"
# ----------------------------------------

# ----------  Model  ----------
class ResidualBlock(nn.Module):
    def __init__(self, ch):
        super().__init__()
        self.conv1 = nn.Conv2d(ch, ch, 3, 1, 1, bias=False)
        self.bn1   = nn.BatchNorm2d(ch)
        self.conv2 = nn.Conv2d(ch, ch, 3, 1, 1, bias=False)
        self.bn2   = nn.BatchNorm2d(ch)

    def forward(self, x):
        out = F.relu(self.bn1(self.conv1(x)))
        out = self.bn2(self.conv2(out))
        return F.relu(x + out)

class HexResNet(nn.Module):
    def __init__(self, ch=CHANNELS, blocks=RES_BLOCKS):
        super().__init__()
        self.stem = nn.Sequential(
            nn.Conv2d(3, ch, 3, 1, 1, bias=False),
            nn.BatchNorm2d(ch),
            nn.ReLU(inplace=True)
        )
        self.body = nn.Sequential(*[ResidualBlock(ch) for _ in range(blocks)])
        # policy head
        self.head_p = nn.Sequential(
            nn.Conv2d(ch, 32, 1), nn.ReLU(inplace=True),
            nn.Flatten(), nn.Linear(32 * 9 * 9, 81)
        )
        # value head
        self.head_v = nn.Sequential(
            nn.Conv2d(ch, 32, 1), nn.ReLU(inplace=True),
            nn.AdaptiveAvgPool2d(1), nn.Flatten(),
            nn.Linear(32, 1), nn.Tanh()
        )

    def forward(self, x, mask=None):
        x = self.stem(x)
        x = self.body(x)
        p = self.head_p(x)
        if mask is not None:
            p = p.masked_fill(mask == 0, -1e9)  # 掩掉非法格
        v = self.head_v(x)
        return p, v
# -----------------------------

def load_csv(csv_path: str):
    print("Reading CSV…")
    df = pd.read_csv(csv_path, header=None, dtype="int8")  # 243+2 列
    x = torch.tensor(df.iloc[:, :243].values, dtype=torch.float32)\
            .view(-1, 3, 9, 9)                # (N,3,9,9)
    move = torch.tensor(df.iloc[:, 243].values, dtype=torch.long)
    z    = torch.tensor(df.iloc[:, 244].values, dtype=torch.float32)\
            .unsqueeze(1)                     # (N,1)
    return TensorDataset(x, move, z)

def get_mask(batch_x):
    """plane0+1+2 >0 的格子视为存在"""
    b = batch_x.sum(1)       # (B,9,9)
    return (b.flatten(1) != 0).float()  # (B,81)

def train_one_epoch(net, loader, opt, scaler):
    net.train()
    pbar = tqdm(loader, desc="train", leave=False)
    for x, move, z in pbar:
        x, move, z = x.to(DEVICE), move.to(DEVICE), z.to(DEVICE)
        mask = get_mask(x).to(DEVICE)
        with autocast():
            logits, v_hat = net(x, mask)
            loss_p = F.cross_entropy(logits, move)
            loss_v = F.mse_loss(v_hat, z)
            loss   = loss_p + loss_v
        opt.zero_grad()
        scaler.scale(loss).backward()
        scaler.unscale_(opt)
        torch.nn.utils.clip_grad_norm_(net.parameters(), 1.0)
        scaler.step(opt); scaler.update()
        pbar.set_postfix({"loss": f"{loss.item():.3f}"})

@torch.no_grad()
def eval_loss(net, loader):
    net.eval(); total = 0; n = 0
    for x, move, z in loader:
        x, move, z = x.to(DEVICE), move.to(DEVICE), z.to(DEVICE)
        logits, v_hat = net(x, get_mask(x).to(DEVICE))
        loss = F.cross_entropy(logits, move) + F.mse_loss(v_hat, z)
        total += loss.item() * x.size(0); n += x.size(0)
    return total / n

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--csv", default="dataset.csv")
    parser.add_argument("--out", default="hex_cnn.pt")
    args = parser.parse_args()

    ds = load_csv(args.csv)
    n_train = int(len(ds) * 0.95)
    train_set, val_set = torch.utils.data.random_split(ds, [n_train, len(ds)-n_train])
    train_ld = DataLoader(train_set, batch_size=BATCH, shuffle=True,
                          num_workers=8, pin_memory=True)
    val_ld   = DataLoader(val_set, batch_size=8192, shuffle=False,
                          num_workers=4, pin_memory=True)

    net = HexResNet().to(DEVICE)
    opt = torch.optim.AdamW(net.parameters(), lr=LR, weight_decay=WEIGHT_DEC)
    scaler = GradScaler()

    best = math.inf
    for epoch in range(1, EPOCHS+1):
        t0 = time.time()
        train_one_epoch(net, train_ld, opt, scaler)
        val_loss = eval_loss(net, val_ld)
        dt = time.time() - t0
        print(f"Epoch {epoch}/{EPOCHS}  val_loss={val_loss:.4f}  time={dt:.1f}s")
        if val_loss < best:
            best = val_loss
            torch.save(net.state_dict(), args.out)
            print(f"  ↳ saved weights to {args.out}")

    # TorchScript 导出（可选）
    net.load_state_dict(torch.load(args.out, map_location=DEVICE))
    net.eval()
    example = torch.randn(1, 3, 9, 9).to(DEVICE)
    traced = torch.jit.trace(net, (example, None))
    traced.save(os.path.splitext(args.out)[0] + ".ts")
    print("TorchScript saved!")

if __name__ == "__main__":
    main()
