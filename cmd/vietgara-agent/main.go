// Command vietgara-agent is VietGara's print agent (FR-DOC-08, Release
// 13.1): installed on a garage PC, it prints the documents sent from the web
// console and the apps on the PC's printers, without a dialog.
//
//	vietgara-agent setup [--server https://app.vietgara.vn] ABCD-EFGH
//	                         pair with a code from VietGara, then install and
//	                         start the Windows service (as an administrator)
//	vietgara-agent pair [--server URL] CODE   pair only
//	vietgara-agent run       prints from this window (Ctrl+C stops it)
//	vietgara-agent install | uninstall        the Windows service
//	vietgara-agent status    what the agent is paired with, and its printers
//	vietgara-agent version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/viettechno/vietgara-backend/internal/printagent/agent"
	"github.com/viettechno/vietgara-backend/internal/printagent/spool"
)

// version is set at build time: -ldflags "-X main.version=0.1.0".
var version = "dev"

const defaultServer = "https://app.vietgara.vn"

// The messages of the command, for the garage's staff.
var (
	errUnknownCommand = errors.New("không có lệnh này")
	errNoCode         = errors.New("hãy nhập mã ghép nối, ví dụ: vietgara-agent setup ABCD-EFGH")
	errWrongCode      = errors.New("mã ghép nối sai hoặc đã hết hạn (mã dùng được 15 phút, một lần): tạo mã mới trên VietGara")
)

// say writes a line for the person at the terminal.
func say(out io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(out, format+"\n", args...)
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		say(os.Stderr, "Lỗi: %v", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if isService() {
		return runService()
	}

	if len(args) == 0 {
		usage(out)

		return nil
	}

	cmd, rest := args[0], args[1:]

	switch cmd {
	case "setup":
		return setup(rest, out)
	case "pair":
		_, err := pair(rest, out)

		return err
	case "run":
		return foreground()
	case "install":
		return installService(out)
	case "uninstall":
		return uninstallService(out)
	case "status":
		return status(out)
	case "version":
		say(out, "vietgara-agent %s", version)

		return nil
	case "help", "-h", "--help":
		usage(out)

		return nil
	}

	usage(out)

	return fmt.Errorf("%w: %q", errUnknownCommand, cmd)
}

func usage(out io.Writer) {
	_, _ = fmt.Fprint(out, `Tác nhân in VietGara `+version+`

  vietgara-agent setup [--server https://app.vietgara.vn] MÃ-GHÉP-NỐI
      ghép nối với gara bằng mã tạo trên VietGara, rồi cài và chạy dịch vụ
      Windows (mở Command Prompt bằng "Run as administrator")
  vietgara-agent pair [--server URL] MÃ   chỉ ghép nối
  vietgara-agent run         chạy trong cửa sổ này (Ctrl+C để dừng)
  vietgara-agent install     cài dịch vụ Windows
  vietgara-agent uninstall   gỡ dịch vụ Windows
  vietgara-agent status      xem tác nhân đang ghép với gara nào và các máy in
  vietgara-agent version
`)
}

func stateStore() *agent.Store { return &agent.Store{Path: statePath()} }

func pair(args []string, out io.Writer) (agent.State, error) {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(out)
	server := fs.String("server", defaultServer, "the address of VietGara")

	if err := fs.Parse(args); err != nil {
		return agent.State{}, err
	}

	if fs.NArg() != 1 {
		return agent.State{}, errNoCode
	}

	store := stateStore()
	if err := prepareStateDir(store); err != nil {
		return agent.State{}, err
	}

	st, err := agent.Pair(context.Background(), store, *server, fs.Arg(0), version)
	if errors.Is(err, agent.ErrPairingCode) {
		return agent.State{}, errWrongCode
	}

	if err != nil {
		return agent.State{}, err
	}

	say(out, "Đã ghép nối \"%s\" với gara %s.", st.AgentName, st.GarageName)

	return st, nil
}

func setup(args []string, out io.Writer) error {
	if _, err := pair(args, out); err != nil {
		return err
	}

	if !serviceSupported {
		say(out, "Chạy tác nhân bằng: vietgara-agent run")

		return nil
	}

	return installService(out)
}

func status(out io.Writer) error {
	st, err := stateStore().Load()
	if errors.Is(err, agent.ErrNotPaired) {
		say(out, "Chưa ghép nối. Chạy: vietgara-agent setup MÃ-GHÉP-NỐI")

		return nil
	}

	if err != nil {
		return err
	}

	say(out, "Tác nhân: %s\nGara: %s\nMáy chủ: %s\nDịch vụ: %s\nMáy in của máy tính này:",
		st.AgentName, st.GarageName, st.Server, serviceStatus())

	printers, err := spool.New(spool.Options{}).Printers(context.Background())
	if err != nil {
		return err
	}

	for _, p := range printers {
		mark := ""
		if p.Default {
			mark = " (mặc định)"
		}

		say(out, "  - %s%s", p.Name, mark)
	}

	return nil
}

// foreground runs the agent in this window until Ctrl+C.
func foreground() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return newAgent(slog.New(slog.NewTextHandler(os.Stderr, nil))).Run(ctx)
}

func newAgent(log *slog.Logger) *agent.Agent {
	return &agent.Agent{State: stateStore(), Spooler: spool.New(spool.Options{}), Version: version, Log: log}
}
