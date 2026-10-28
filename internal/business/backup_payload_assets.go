package business

// ValidateOfflineBackupPayloadAssets checks a decrypted, bounded JSON payload
// for the external file/storage dependencies unsupported by native backup V1.
// The caller must authenticate/decrypt the payload with its own format reader
// first. It never opens a path, returns payload content, or changes runtime state.
func ValidateOfflineBackupPayloadAssets(raw []byte) error {
	return backupCheckJSON(raw)
}
