#!/usr/bin/env python3
import os
from mutagen.mp3 import MP3

def get_mp3_durations(audio_dir):
    """
    扫描 audio_dir 下所有 .mp3 文件，返回 {filename: duration_seconds}
    """
    durations = {}
    for fname in os.listdir(audio_dir):
        if fname.lower().endswith(".mp3"):
            path = os.path.join(audio_dir, fname)
            try:
                audio = MP3(path)
                durations[fname] = audio.info.length
            except Exception as e:
                print(f"无法读取 {fname}: {e}")
    return durations

if __name__ == "__main__":
    audio_dir = "./"
    durs = get_mp3_durations(audio_dir)
    for fname, secs in durs.items():
        ms = int(secs * 1000)
        print(f"{fname}: {secs:.3f} 秒 ({ms} 毫秒)")
