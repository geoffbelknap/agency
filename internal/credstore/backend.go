package credstore

// SecretBackend is the swap boundary for credential storage. Implement this
// for Vault, AWS Secrets Manager, Azure Key Vault, GCP Secret Manager, or
// any other secret manager. The built-in FileBackend implements it with
// AES-256-GCM encryption.
//
// The backend stores raw strings for values and flat key-value metadata.
// It knows nothing about Entry or Metadata structs -- the Store layer
// handles serialization.
type SecretBackend interface {
	Put(name, value string, metadata map[string]string) error
	Get(name string) (value string, metadata map[string]string, err error)
	Delete(name string) error
	List() ([]SecretRef, error)
}
