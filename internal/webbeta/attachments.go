package webbeta

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
)

type Upload struct {
	Name string `json:"name"`
	MIME string `json:"mime"`
	Data []byte `json:"data"`
}
type snapshot struct {
	owner   string
	request string
	upload  Upload
	digest  string
}

// Vault holds immutable, explicitly authorized snapshots, not arbitrary paths.
// Grants are scoped to one send operation, so migration cannot silently reupload.
type Vault struct {
	mu     sync.Mutex
	items  map[string]snapshot
	closed bool
}

func NewVault() *Vault { return &Vault{items: make(map[string]snapshot)} }
func (v *Vault) Grant(owner, request, name string, raw []byte) (string, error) {
	if owner == "" || request == "" || len(raw) == 0 || len(raw) > MaxAttachmentBytes {
		return "", errors.New("invalid attachment scope or size")
	}
	name = filepath.Base(name)
	if name == "." || strings.ContainsAny(name, "\x00\r\n") {
		return "", errors.New("invalid attachment name")
	}
	mime := http.DetectContentType(raw)
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png":
		if mime != "image/png" {
			return "", errors.New("image signature mismatch")
		}
	case ".jpg", ".jpeg":
		if mime != "image/jpeg" {
			return "", errors.New("image signature mismatch")
		}
	case ".webp":
		if mime != "image/webp" {
			return "", errors.New("image signature mismatch")
		}
	case ".pdf":
		if mime != "application/pdf" {
			return "", errors.New("PDF signature mismatch")
		}
	case ".txt", ".md", ".csv", ".json":
		if !strings.HasPrefix(mime, "text/plain") {
			return "", errors.New("file is not plain text")
		}
	default:
		return "", errors.New("file type not enabled for beta; use PNG, JPEG, WebP, PDF or plain text")
	}
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return "", ErrIdentityClosed
	}
	var total int
	count := 0
	for id, s := range v.items {
		if s.owner == owner && s.request == request {
			if s.digest == digest && s.upload.Name == name {
				return id, nil
			}
			total += len(s.upload.Data)
			count++
		}
	}
	if total+len(raw) > MaxBatchBytes || count >= 5 {
		return "", errors.New("attachment batch exceeds beta limit")
	}
	id := newID()
	v.items[id] = snapshot{owner, request, Upload{name, mime, append([]byte(nil), raw...)}, digest}
	return id, nil
}
func (v *Vault) Resolve(owner, request string, ids []string) ([]Upload, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return nil, ErrIdentityClosed
	}
	var out []Upload
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		s, ok := v.items[id]
		if !ok || s.owner != owner || s.request != request {
			return nil, errors.New("attachment authorization missing or expired")
		}
		u := s.upload
		u.Data = append([]byte(nil), u.Data...)
		out = append(out, u)
	}
	return out, nil
}

// Invalidate revokes every grant permanently; an old adapter cannot refill it.
func (v *Vault) Invalidate() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.closed = true
	clear(v.items)
}
func (v *Vault) Release(owner, request string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for id, s := range v.items {
		if s.owner == owner && s.request == request {
			delete(v.items, id)
		}
	}
}
