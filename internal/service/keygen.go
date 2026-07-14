package service

// KeyGenerator creates a WireGuard private/public key pair.
type KeyGenerator func() (privateKey, publicKey string, err error)
