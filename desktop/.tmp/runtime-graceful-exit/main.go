package main

import (
 "fmt"
 "os"

 "github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/internal/installipc"
)

func main() {
 const target = `D:\AI-Reasonix\.tmp\orca-beta-worktree\.tmp\beta2-candidate-20261115-r2`
 sent, err := installipc.RequestShutdown(target)
 if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
 if !sent { fmt.Fprintln(os.Stderr, "No matching listener"); os.Exit(2) }
 fmt.Println("Graceful shutdown requested for isolated R2 candidate only")
}
