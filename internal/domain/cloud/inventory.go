package cloud

// The inventory marker's literal session id and the bound on how many ids it
// may carry, `CloudAppBridge.swift:1373-1374`. The publisher writes the marker
// and the viewer client reads it; the hosted console holds the same two values
// once, in `web/console/src/legacy/js/net/client.js`.
const (
	InventorySessionID = "__clawdline_inventory_v1__"
	InventoryLimit     = 512
)
