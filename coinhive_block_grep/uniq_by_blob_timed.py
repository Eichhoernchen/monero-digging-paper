#!/usr/bin/env python

import json
import sys
import datetime

x = {}
last_check=datetime.datetime.now()

deletetime = datetime.timedelta(minutes=20)
#deletetime = datetime.timedelta(seconds=5)
checktime = datetime.timedelta(minutes=5)
#checktime = datetime.timedelta(seconds=1)

for line in sys.stdin:
    #    jline = json.loads(line)
    #    b = jline["params"]["blob"]
    if line in x:
        continue
    x[line] = datetime.datetime.now()
    sys.stdout.write(line)
    sys.stdout.flush()
    ###
    if x[line] - last_check > checktime:
        last_check = x[line]
        for k in x.keys():
            if last_check - x[k] > deletetime:
                del x[k]
#                sys.stderr.write("Removing from uniq mem\n")
