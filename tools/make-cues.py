"""Render Ridge Radio's original narrow-band handset cues from oscillators."""
import math
import struct
import wave
from pathlib import Path

dest = Path(__file__).resolve().parents[1] / 'android/app/src/main/res/raw'
# (frequency in Hz, length in ms), with deliberate gaps for the permission doublet.
scores = {
    'grant': [(1680, 44), (0, 18), (2050, 71)],
    'incoming': [(1330, 39), (0, 13), (1620, 43)],
    'free': [(1220, 40), (950, 36)],
    'denied': [(450, 85), (0, 42), (380, 116)],
}
rate = 24000
for name, notes in scores.items():
    samples = bytearray()
    for freq, ms in notes:
        count = rate * ms // 1000
        for i in range(count):
            env = min(1, i / 85, (count - 1 - i) / 110)
            angle = 2 * math.pi * freq * i / rate
            v = (math.sin(angle) + 0.17 * math.sin(2 * angle)) * env
            samples.extend(struct.pack('<h', round(v * 10200)))
    with wave.open(str(dest / (name + '.wav')), 'wb') as out:
        out.setparams((1, 2, rate, 0, 'NONE', 'not compressed'))
        out.writeframes(samples)
