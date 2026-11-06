// Command pgwash は PostgreSQL 洗い替えツールのエントリポイントである。
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/terayu/pgwash/internal/cli"
)

func main() {
	os.Exit(run())
}

// run は実処理を行い終了コードを返す。os.Exit は defer を実行しないため、
// main() 側で os.Exit するだけに留め、実処理は本関数へ切り出す（設計案11章）。
func run() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return cli.Execute(ctx)
}
