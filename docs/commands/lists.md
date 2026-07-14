# List Commands

::: info v0.1.0 — Real
Every command on this page is implemented today.
:::

| Command | Summary |
|---|---|
| `LPUSH` | Push one or more values onto the head of a list |
| `RPUSH` | Push one or more values onto the tail of a list |
| `LPOP` | Pop a value from the head of a list |
| `RPOP` | Pop a value from the tail of a list |
| `LRANGE` | Get a range of elements from a list |
| `LLEN` | Get the length of a list |
| `LINDEX` | Get an element from a list by index |
| `LSET` | Set the value of a list element by index |
| `LREM` | Remove matching elements from a list |

Negative indexes are supported everywhere an index is accepted (`LRANGE`,
`LINDEX`, `LSET`), counting from the tail of the list (`-1` is the last
element), matching Redis semantics. Popping the last element out of a list
removes the key entirely.

## Example

```bash
redis-cli -p 6380 RPUSH queue job-a job-b job-c
# (integer) 3
redis-cli -p 6380 LRANGE queue 0 -1
# 1) "job-a"
# 2) "job-b"
# 3) "job-c"
redis-cli -p 6380 LPOP queue
# "job-a"
redis-cli -p 6380 LREM queue 0 "job-b"
# (integer) 1
```
