#!/usr/bin/env python3
import torch
from torch.utils.data import TensorDataset, DataLoader
import pandas as pd

# 直接用 pandas/C 引擎读 CSV，比 JSON 快十几倍
df = pd.read_csv("features.csv", header=None).values
X = torch.tensor(df[:, :-1], dtype=torch.float32)
y = torch.tensor(df[:,  -1], dtype=torch.float32)

dataset = TensorDataset(X, y)
loader  = DataLoader(dataset, batch_size=256, shuffle=True, pin_memory=True)

# 然后照常训练
model = torch.nn.Linear(11, 1).cuda()
opt   = torch.optim.Adam(model.parameters(), lr=1e-3)
loss_fn = torch.nn.MSELoss()

for epoch in range(15):
    total, n = 0.0, 0
    for xb, yb in loader:
        pred = model(xb.cuda()).squeeze(1)
        loss = loss_fn(pred, yb.cuda())
        opt.zero_grad(); loss.backward(); opt.step()
        total += loss.item()*len(xb); n += len(xb)
    print(f"Epoch {epoch+1} loss={total/n:.4f}")

ws = model.weight.data.cpu().numpy().flatten()
b  = model.bias.data.item()
print("\n// Learned parameters, copy into your Go evaluate.go:")
print("var learnedW = []float64{")
for w in ws:
    print(f"    {w:.8f},")
print("}")
print(f"const learnedB = {b:.16f}")
print()
