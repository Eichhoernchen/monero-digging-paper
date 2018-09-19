#!/usr/bin/env python

import json
import sys



x = set()

for line in sys.stdin:
    #    jline = json.loads(line)
    #    b = jline["params"]["blob"]
    if line in x:
        continue
    x.add(line)
    sys.stdout.write(line)
