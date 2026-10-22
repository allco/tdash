#!/bin/sh
# Prints made-up metrics as JSON: two load series for the spline, and 300 latency samples for
# the histogram.
awk -v seed="$(date +%s)" 'BEGIN {
  srand(seed)
  cpu = 30 + rand() * 40; mem = 50 + rand() * 20
  printf "{\"load\": {\"cpu\": ["
  for (i = 0; i < 40; i++) {
    cpu += rand() * 20 - 10; if (cpu < 2) cpu = 2; if (cpu > 98) cpu = 98
    printf "%s%.1f", (i ? "," : ""), cpu
  }
  printf "], \"memory\": ["
  for (i = 0; i < 40; i++) {
    mem += rand() * 6 - 3; if (mem < 2) mem = 2; if (mem > 98) mem = 98
    printf "%s%.1f", (i ? "," : ""), mem
  }
  printf "]}, \"latency\": ["
  for (i = 0; i < 300; i++) {
    v = 0; for (k = 0; k < 6; k++) v += rand()
    printf "%s%.0f", (i ? "," : ""), 40 + v * 60
  }
  print "]}"
}'
