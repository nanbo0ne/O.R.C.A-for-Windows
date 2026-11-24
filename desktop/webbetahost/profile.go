package webbetahost

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const profileOwner = "orca-deepseek-web-profile-v1\n"

// Profile owns a fixed child of the beta data root, never a workspace, ORCA
// account store, generic browser profile, or a caller-supplied deletion path.
type Profile struct{ root string }

func OpenProfile(betaRoot string) (*Profile, error) {
	if !filepath.IsAbs(betaRoot) || filepath.Clean(betaRoot) == filepath.VolumeName(betaRoot)+string(filepath.Separator) {
		return nil, errors.New("Web Beta requires an absolute beta data root")
	}
	if err := rejectLinks(betaRoot); err != nil {
		return nil, err
	}
	p := &Profile{root: filepath.Join(filepath.Clean(betaRoot), "deepseek-web")}
	if err := rejectLinks(p.root); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(p.root, 0700); err != nil {
		return nil, err
	}
	marker := filepath.Join(p.root, ".orca-profile-owner")
	if err := rejectLinks(marker); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(marker)
	if errors.Is(err, os.ErrNotExist) {
		entries, readErr := os.ReadDir(p.root)
		if readErr != nil {
			return nil, readErr
		}
		if len(entries) != 0 {
			return nil, errors.New("refusing to adopt an unowned DeepSeek directory")
		}
		f, createErr := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr != nil {
			return nil, createErr
		}
		_, err = f.WriteString(profileOwner)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
	} else if err == nil && string(raw) != profileOwner {
		err = errors.New("invalid DeepSeek profile ownership marker")
	}
	if err != nil {
		return nil, err
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Profile) BrowserPath() string { return filepath.Join(p.root, "profile") }
func (p *Profile) StatePath() string   { return filepath.Join(p.root, "web-state.json") }

func rejectLinks(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && profileLink(info) {
			return errors.New("DeepSeek profile paths may not contain symlinks or junctions")
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

func (p *Profile) validate() error {
	if p == nil || !filepath.IsAbs(p.root) || filepath.Base(p.root) != "deepseek-web" {
		return errors.New("invalid owned profile")
	}
	if err := rejectLinks(p.BrowserPath()); err != nil {
		return err
	}
	if err := rejectLinks(p.StatePath()); err != nil {
		return err
	}
	marker := filepath.Join(p.root, ".orca-profile-owner")
	if err := rejectLinks(marker); err != nil {
		return err
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		return err
	}
	if string(raw) != profileOwner {
		return errors.New("profile ownership verification failed")
	}
	return nil
}

func (p *Profile) Ensure() error {
	if err := p.validate(); err != nil {
		return err
	}
	return os.MkdirAll(p.BrowserPath(), 0700)
}

// Called only after old tasks/native handles drain and the pending account
// transition is durable. This removes no website login or chat data.
func (p *Profile) PrepareLoginAdoption() error {
	if err := p.validate(); err != nil {
		return err
	}
	marker := filepath.Join(p.BrowserPath(), identityMarker)
	if err := rejectLinks(marker); err != nil {
		return err
	}
	err := os.Remove(marker)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Clear is called only after CloseAndWait confirms the own browser exited.
// Reject links before RemoveAll; never follow a junction into another profile.
func (p *Profile) Clear() error {
	if err := p.validate(); err != nil {
		return err
	}
	target := p.BrowserPath()
	rel, err := filepath.Rel(p.root, target)
	if err != nil || rel != "profile" || strings.Contains(rel, "..") {
		return errors.New("profile deletion escaped ownership root")
	}
	err = filepath.WalkDir(target, func(_ string, d fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if profileLink(info) {
			return errors.New("refusing profile cleanup containing a link")
		}
		return nil
	})
	if err != nil {
		return err
	}
	return os.RemoveAll(target)
}
