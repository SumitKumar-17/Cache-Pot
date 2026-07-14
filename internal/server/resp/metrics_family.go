package resp

import "strings"

// coreFamilies maps the flat Redis-style command names to their
// observability family bucket. The "MODULE.ACTION"-style commands
// (CACHE.SEMANTIC, VECTOR.UPSERT, MEMORY.PUT, AGENT.REMEMBER, ...) don't
// need an entry here -- commandFamily derives their family from the module
// prefix instead, since that naming convention makes the family obvious.
var coreFamilies = map[string]string{
	"PING": "connection", "ECHO": "connection", "SELECT": "connection",
	"HELLO": "connection", "AUTH": "connection", "CLIENT": "connection",
	"COMMAND": "connection", "INFO": "connection", "QUIT": "connection",

	"DEL": "generic", "EXISTS": "generic", "EXPIRE": "generic",
	"PEXPIRE": "generic", "TTL": "generic", "PTTL": "generic",
	"PERSIST": "generic", "TYPE": "generic", "KEYS": "generic",
	"SCAN": "generic", "RENAME": "generic", "FLUSHDB": "generic",
	"FLUSHALL": "generic",

	"GET": "strings", "SET": "strings", "MGET": "strings", "MSET": "strings",
	"INCR": "strings", "INCRBY": "strings", "DECR": "strings",
	"DECRBY": "strings", "APPEND": "strings", "STRLEN": "strings",

	"HSET": "hash", "HGET": "hash", "HGETALL": "hash", "HDEL": "hash",
	"HEXISTS": "hash", "HKEYS": "hash", "HVALS": "hash", "HLEN": "hash",
	"HMGET": "hash", "HINCRBY": "hash",

	"LPUSH": "list", "RPUSH": "list", "LPOP": "list", "RPOP": "list",
	"LRANGE": "list", "LLEN": "list", "LINDEX": "list", "LSET": "list",
	"LREM": "list",

	"SADD": "set", "SREM": "set", "SMEMBERS": "set", "SISMEMBER": "set",
	"SCARD": "set", "SINTER": "set", "SUNION": "set", "SDIFF": "set",

	"ZADD": "sorted-set", "ZREM": "sorted-set", "ZRANGE": "sorted-set",
	"ZREVRANGE": "sorted-set", "ZSCORE": "sorted-set", "ZRANK": "sorted-set",
	"ZCARD": "sorted-set", "ZINCRBY": "sorted-set", "ZRANGEBYSCORE": "sorted-set",

	"SUBSCRIBE": "pubsub", "UNSUBSCRIBE": "pubsub", "PUBLISH": "pubsub",
	"PSUBSCRIBE": "pubsub",

	"MULTI": "transactions", "EXEC": "transactions", "DISCARD": "transactions",
	"WATCH": "transactions", "UNWATCH": "transactions",
}

// moduleFamilies maps a "MODULE" prefix (the part before the "." in a
// command like CACHE.SEMANTIC) to its observability family. AGENT and
// MEMORY share a family since AGENT.REMEMBER/AGENT.RECALL are just
// ergonomic wrappers over the same MemoryStore MEMORY.* uses.
var moduleFamilies = map[string]string{
	"CACHE":  "semantic-cache",
	"TOOL":   "tool-cache",
	"VECTOR": "vector",
	"MEMORY": "agent-memory",
	"AGENT":  "agent-memory",
}

// commandFamily derives the observability grouping for a command name, so
// per-family latency can be recorded once at the central dispatch point
// (see conn.go) instead of being hand-instrumented into every handler file.
func commandFamily(name string) string {
	upper := strings.ToUpper(name)
	if i := strings.IndexByte(upper, '.'); i >= 0 {
		if fam, ok := moduleFamilies[upper[:i]]; ok {
			return fam
		}
		return strings.ToLower(upper[:i])
	}
	if fam, ok := coreFamilies[upper]; ok {
		return fam
	}
	return "other"
}
