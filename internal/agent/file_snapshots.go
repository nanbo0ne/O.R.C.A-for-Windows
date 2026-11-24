package agent

import "github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"

func cloneFileSnapshots(files []provider.FileContent) []provider.FileContent {
	if files == nil {
		return nil
	}
	out := append([]provider.FileContent(nil), files...)
	for i := range out {
		out[i].Data = append([]byte(nil), out[i].Data...)
	}
	return out
}

func cloneMessageFileSnapshots(messages []provider.Message) []provider.Message {
	out := append([]provider.Message(nil), messages...)
	for i := range out {
		out[i].Files = cloneFileSnapshots(out[i].Files)
	}
	return out
}
