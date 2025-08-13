import argparse, torch, torch.nn as nn, torch.nn.functional as F
import onnx, onnxruntime as ort
import numpy as np

# ------- 模型结构（和训练一致） -------
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
        self.stem = nn.Sequential(nn.Conv2d(3, ch, 3, 1, 1, bias=False),
                                  nn.BatchNorm2d(ch), nn.ReLU(inplace=True))
        self.body = nn.Sequential(*[ResidualBlock(ch) for _ in range(blocks)])
        self.head_p = nn.Sequential(nn.Conv2d(ch, 32, 1), nn.ReLU(inplace=True),
                                    nn.Flatten(), nn.Linear(32*9*9, 81))
        self.head_v = nn.Sequential(nn.Conv2d(ch, 32, 1), nn.ReLU(inplace=True),
                                    nn.AdaptiveAvgPool2d(1), nn.Flatten(),
                                    nn.Linear(32, 1), nn.Tanh())
    def forward(self, x, mask=None):
        x = self.stem(x); x = self.body(x)
        p = self.head_p(x)   # (B,81)
        v = self.head_v(x)   # (B,1)
        return p, v

class WrapNoMask(nn.Module):
    def __init__(self, net):
        super().__init__()
        self.net = net
    def forward(self, x):
        p, v = self.net(x, None)
        return p, v

def strip_module_prefix(sd):
    return { (k[7:] if k.startswith("module.") else k): v for k, v in sd.items() }

def infer_arch(sd):
    # 通道数：看 stem 第一层输出通道
    ch = int(sd["stem.0.weight"].shape[0])
    # 残差块数：统计 body.N.c1.weight 的 N 最大值
    blocks = 0
    for k in sd.keys():
        if k.startswith("body.") and k.endswith(".c1.weight"):
            try:
                n = int(k.split(".")[1]); blocks = max(blocks, n+1)
            except: pass
    if blocks == 0:
        # 兼容没触发上面逻辑的情况（比如空 body）：至少 1
        blocks = 1
    return ch, blocks

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--pt", required=True, help="PyTorch权重 .pt（state_dict 或整模型）")
    ap.add_argument("--onnx", required=True, help="输出 onnx 路径")
    ap.add_argument("--channels", type=int, default=0, help="主干通道，不填则自动推断")
    ap.add_argument("--blocks",   type=int, default=0, help="残差块数，不填则自动推断")
    ap.add_argument("--opset",    type=int, default=17)
    args = ap.parse_args()

    obj = torch.load(args.pt, map_location="cpu")
    if isinstance(obj, dict) and "state_dict" in obj:
        sd = strip_module_prefix(obj["state_dict"])
    elif isinstance(obj, dict):
        sd = strip_module_prefix(obj)
    else:
        # 可能保存的是整模型；尽量取其 state_dict
        try:
            sd = strip_module_prefix(obj.state_dict())
        except Exception as e:
            raise RuntimeError(f"无法从 {args.pt} 提取 state_dict: {e}")

    ch, blocks = args.channels, args.blocks
    if ch == 0 or blocks == 0:
        auto_ch, auto_blocks = infer_arch(sd)
        if ch == 0: ch = auto_ch
        if blocks == 0: blocks = auto_blocks
    print(f"[export] using architecture: channels={ch}, blocks={blocks}")

    net = HexResNet(ch=ch, blocks=blocks).eval()
    missing, unexpected = net.load_state_dict(sd, strict=False)
    if missing or unexpected:
        print("[warn] missing keys:", missing)
        print("[warn] unexpected keys:", unexpected)

    wrapper = WrapNoMask(net).eval()
    dummy = torch.randn(1, 3, 9, 9)  # (B,3,9,9)

    torch.onnx.export(
        wrapper, dummy, args.onnx,
        input_names=["x"], output_names=["logits", "value"],
        dynamic_axes={"x": {0: "batch"}, "logits": {0: "batch"}, "value": {0: "batch"}},
        do_constant_folding=True, opset_version=args.opset
    )
    onnx_model = onnx.load(args.onnx); onnx.checker.check_model(onnx_model)
    print(f"[export] saved ONNX to {args.onnx}")

    # quick self-test (CPU EP)
    sess = ort.InferenceSession(args.onnx, providers=["CPUExecutionProvider"])
    out_logits, out_value = sess.run(["logits","value"], {"x": dummy.numpy().astype(np.float32)})
    print(f"[check] logits shape={np.array(out_logits).shape}, value shape={np.array(out_value).shape}")

if __name__ == "__main__":
    main()
