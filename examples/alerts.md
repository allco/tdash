# 3 firing

- {bold red}CRITICAL{/} db-primary: replication lag **42s**
- {yellow}WARNING{/} api: p99 above 800ms for 10m
- {yellow}WARNING{/} disk `/var` at 87%

> Silenced: {dim}nightly-backup (until 06:00){/}

---

Runbook: [wiki/oncall](https://example.com/oncall)

```
$ kubectl -n prod rollout restart deploy/api --context prod-eu-west-1 # a long line that scrolls sideways
```
