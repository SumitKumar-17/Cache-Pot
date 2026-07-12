package resp

// RegisterAll builds the full Phase 1 command table onto r. server.go calls
// this once at startup before accepting connections.
func RegisterAll(r *Registry) {
	RegisterConn(r)
	RegisterGeneric(r)
	RegisterString(r)
	RegisterHash(r)
	RegisterList(r)
	RegisterSet(r)
	RegisterZSet(r)
	RegisterPubSub(r)
	RegisterTx(r)
	RegisterSemantic(r)
	RegisterToolCache(r)
	RegisterVector(r)
}
