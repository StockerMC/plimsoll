# Holds the curve's value for one step per row, rows counted from the start
# time so every pod plays the same row at once, and loops. Memory is held in
# 1 MiB chunks so a resize never frees more than it needs to.
import time

step = float(open("/curve/step").read())
start = float(open("/curve/start").read())
mib = [float(l) for l in open("/curve/curve.csv") if not l.startswith("#")]
chunks = []
while True:
    i = max(int((time.time() - start) // step), 0) % len(mib)
    n = round(mib[i])
    while len(chunks) < n:
        chunks.append(b"x" * (1 << 20))
    del chunks[n:]
    time.sleep(step - (time.time() - start) % step)
