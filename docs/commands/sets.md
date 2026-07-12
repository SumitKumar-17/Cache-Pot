# Set Commands

::: info Phase 1 — Real
Every command on this page is implemented today.
:::

| Command | Summary |
|---|---|
| `SADD` | Add one or more members to a set |
| `SREM` | Remove one or more members from a set |
| `SMEMBERS` | Get all members of a set |
| `SISMEMBER` | Check whether a value is a member of a set |
| `SCARD` | Get the number of members in a set |
| `SINTER` | Get the intersection of multiple sets |
| `SUNION` | Get the union of multiple sets |
| `SDIFF` | Get the difference of multiple sets |

Removing the last member of a set removes the key entirely.

## Example

```bash
redis-cli -p 6380 SADD tags:post1 go redis cache
# (integer) 3
redis-cli -p 6380 SADD tags:post2 go vector cache
# (integer) 3
redis-cli -p 6380 SINTER tags:post1 tags:post2
# 1) "go"
# 2) "cache"
redis-cli -p 6380 SDIFF tags:post1 tags:post2
# 1) "redis"
```
