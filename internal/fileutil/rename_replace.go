package fileutil

// RenameReplace publishes a completed sibling temporary file by rename only.
// Windows sharing failures are retried briefly; other failures return directly.
// It never deletes or truncates dest and leaves tmp available for recovery on
// failure. The caller must finish and close tmp first, and sync it when crash
// durability is required; this helper does not add filesystem durability.
func RenameReplace(tmp, dest string) error {
	return renameReplace(tmp, dest, nil)
}

// RenameReplaceChecked validates the destination immediately before each rename
// attempt, including retries. Validation failures return directly, even when
// they are permission errors. The caller owns serialization with other writers;
// validation and rename are not a filesystem compare-and-swap.
func RenameReplaceChecked(tmp, dest string, validate func() error) error {
	return renameReplace(tmp, dest, validate)
}
