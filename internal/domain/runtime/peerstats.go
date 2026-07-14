package runtime

import "time"

// PeerStats is the portable WireGuard peer statistics snapshot.
type PeerStats struct {
	PublicKey     string
	LastHandshake time.Time
	RxBytes       int64
	TxBytes       int64
}
