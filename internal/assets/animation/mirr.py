#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Script to generate missing mirrored assets for redClone and redJump.

For each action directory (redClone, redJump) under the base path:
- Flip horizontally (left-right) images from 'upperright' -> 'upperleft' and 'lowerright' -> 'lowerleft'
- Flip vertically (top-bottom) images from 'down' -> 'up'

Usage:
    1. Install Pillow if necessary: pip install pillow
    2. Place this script in Z:\1 (same level as the redClone, redJump folders).
    3. Run: python generate_mirror_assets.py
"""
import sys
from pathlib import Path
from PIL import Image

# Base directory containing the action folders (script's parent directory)
BASE_DIR = Path(__file__).parent.resolve()

# Actions to process
ACTIONS = ['redClone', 'redJump']

# Transform mappings: (source_subdir, dest_subdir, flip_method)
TRANSFORMS = [
    ('upperright', 'upperleft', Image.FLIP_LEFT_RIGHT),
    ('lowerright', 'lowerleft', Image.FLIP_LEFT_RIGHT),
    ('down',       'up',         Image.FLIP_TOP_BOTTOM),
]


def process_action(action: str):
    action_dir = BASE_DIR / action
    if not action_dir.is_dir():
        print(f"Action directory not found: {action_dir}")
        return

    for src_sub, dst_sub, flip_method in TRANSFORMS:
        src_dir = action_dir / src_sub
        dst_dir = action_dir / dst_sub

        if not src_dir.is_dir():
            print(f"  Skipping: source folder does not exist: {src_dir}")
            continue

        dst_dir.mkdir(exist_ok=True)
        print(f"  Processing {src_sub} -> {dst_sub}...")

        for img_path in sorted(src_dir.iterdir()):
            if not img_path.is_file() or img_path.suffix.lower() not in ['.png', '.jpg', '.jpeg']:
                continue

            # Extract frame number from filename
            # e.g. redClone_upperright_001.png -> num = '001'
            stem_parts = img_path.stem.split('_')
            num = stem_parts[-1]
            ext = img_path.suffix

            new_name = f"{action}_{dst_sub}_{num}{ext}"
            out_path = dst_dir / new_name

            if out_path.exists():
                continue

            # Open, flip and save
            try:
                img = Image.open(img_path)
                flipped = img.transpose(flip_method)
                flipped.save(out_path)
                print(f"    Saved: {out_path.name}")
            except Exception as e:
                print(f"    Failed {img_path.name}: {e}")


def main():
    print(f"Base directory: {BASE_DIR}")
    for action in ACTIONS:
        print(f"Processing action: {action}")
        process_action(action)


if __name__ == '__main__':
    try:
        main()
    except KeyboardInterrupt:
        sys.exit(0)
