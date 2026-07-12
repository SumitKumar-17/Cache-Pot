# Sorted Set Commands

::: info Phase 1 — Real
Every command on this page is implemented today.
:::

| Command | Summary |
|---|---|
| `ZADD` | Add one or more scored members to a sorted set |
| `ZREM` | Remove one or more members from a sorted set |
| `ZRANGE` | Get a range of members by rank, lowest score first |
| `ZREVRANGE` | Get a range of members by rank, highest score first |
| `ZSCORE` | Get the score of a member |
| `ZRANK` | Get the rank of a member, lowest score first |
| `ZCARD` | Get the number of members in a sorted set |
| `ZINCRBY` | Increment a member's score by a given amount |
| `ZRANGEBYSCORE` | Get members within a score range |

Members are ordered by score, breaking ties by member name (lexicographic),
matching Redis's tie-breaking rule. Negative indexes are supported in
`ZRANGE`/`ZREVRANGE`, the same as with lists.

## Example

```bash
redis-cli -p 6380 ZADD leaderboard 100 alice 85 bob 95 carol
# (integer) 3
redis-cli -p 6380 ZREVRANGE leaderboard 0 -1 WITHSCORES
# 1) "alice"
# 2) "100"
# 3) "carol"
# 4) "95"
# 5) "bob"
# 6) "85"
redis-cli -p 6380 ZRANK leaderboard bob
# (integer) 0
redis-cli -p 6380 ZRANGEBYSCORE leaderboard 90 100
# 1) "carol"
# 2) "alice"
```
