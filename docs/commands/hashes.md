# Hash Commands

::: info v0.1.0 — Real
Every command on this page is implemented today.
:::

| Command | Summary |
|---|---|
| `HSET` | Set one or more hash field/value pairs |
| `HGET` | Get the value of a hash field |
| `HGETALL` | Get all field/value pairs in a hash |
| `HDEL` | Delete one or more hash fields |
| `HEXISTS` | Check whether a hash field exists |
| `HKEYS` | Get all field names in a hash |
| `HVALS` | Get all values in a hash |
| `HLEN` | Get the number of fields in a hash |
| `HMGET` | Get the values of multiple hash fields |
| `HINCRBY` | Increment an integer hash field by a given amount |

Deleting the last field of a hash removes the key entirely, matching Redis
semantics (an empty hash and a missing key are treated the same way).

## Example

```bash
redis-cli -p 6380 HSET user:1 name "Ada" role "engineer"
# (integer) 2
redis-cli -p 6380 HGET user:1 name
# "Ada"
redis-cli -p 6380 HINCRBY user:1 login_count 1
# (integer) 1
redis-cli -p 6380 HDEL user:1 role
# (integer) 1
```
