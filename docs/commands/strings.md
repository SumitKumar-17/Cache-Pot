# String Commands

::: info v0.1.0 — Real
Every command on this page is implemented today.
:::

| Command | Summary |
|---|---|
| `GET` | Get the value of a key |
| `SET` | Set the value of a key, with optional `EX`/`PX`/`NX`/`XX`/`GET` modifiers |
| `MGET` | Get the values of multiple keys |
| `MSET` | Set multiple key/value pairs atomically |
| `INCR` | Increment an integer value by 1 |
| `INCRBY` | Increment an integer value by a given amount |
| `DECR` | Decrement an integer value by 1 |
| `DECRBY` | Decrement an integer value by a given amount |
| `APPEND` | Append a value to an existing string, creating it if absent |
| `STRLEN` | Get the length of a string value |

## `SET` modifiers

```bash
redis-cli -p 6380 SET key value EX 60      # expire in 60 seconds
redis-cli -p 6380 SET key value PX 60000   # expire in 60000 milliseconds
redis-cli -p 6380 SET key value NX         # only set if key does not exist
redis-cli -p 6380 SET key value XX         # only set if key already exists
redis-cli -p 6380 SET key value GET        # return the previous value
```

`SET ... GET` returns the previous value regardless of whether `NX`/`XX`
ends up blocking the write, matching Redis semantics. If the existing value
isn't a string, `GET` returns a `WRONGTYPE` error rather than silently
overwriting.

## Type errors

Calling a string command against a key holding a non-string value (a hash,
list, set, or sorted set) returns Redis's standard `WRONGTYPE` error rather
than silently succeeding or returning empty:

```bash
redis-cli -p 6380 LPUSH mylist a
redis-cli -p 6380 GET mylist
# (error) WRONGTYPE Operation against a key holding the wrong kind of value
```

## Example

```bash
redis-cli -p 6380 SET counter 10
redis-cli -p 6380 INCRBY counter 5
# (integer) 15
redis-cli -p 6380 APPEND counter "!"
# (integer) 3
redis-cli -p 6380 GET counter
# "15!"
```
