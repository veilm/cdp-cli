package cli

import (
	"fmt"
	"os"
)

func Run() error {
	if len(os.Args) < 2 {
		printUsage()
		return nil
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "help", "--help", "-h":
		printUsage()
		return nil
	case "connect":
		return cmdConnect(args)
	case "eval":
		return cmdEval(args)
	case "wait":
		return cmdWait(args)
	case "wait-visible":
		return cmdWaitVisible(args)
	case "click":
		return cmdClick(args)
	case "hover":
		return cmdHover(args)
	case "drag":
		return cmdDrag(args)
	case "gesture":
		return cmdGesture(args)
	case "key":
		return cmdKey(args)
	case "scroll":
		return cmdScroll(args)
	case "type":
		return cmdType(args)
	case "upload":
		return cmdUpload(args)
	case "inject":
		return cmdInject(args)
	case "dom":
		return cmdDOM(args)
	case "styles":
		return cmdStyles(args)
	case "rect":
		return cmdRect(args)
	case "screenshot":
		return cmdScreenshot(args)
	case "log":
		return cmdLog(args)
	case "network-log":
		return cmdNetworkLog(args)
	case "keep-alive":
		return cmdKeepAlive(args)
	case "tabs":
		return cmdTabs(args)
	case "targets":
		return cmdTargets(args)
	case "disconnect":
		return cmdDisconnect(args)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func printUsage() {
	fmt.Println("cdp - Chrome DevTools CLI helper")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  cdp connect <name> --port 9222 --url https://example")
	fmt.Println("  \t  cdp connect <name> --port 9222 --tab 3")
	fmt.Println("  \t  cdp connect <name> --port 9222 --new [--new-url https://example]")
	fmt.Println("  \t  cdp eval <name> \"JS expression\" [--pretty] [--depth N] [--json] [--wait]")
	fmt.Println("  \t  cdp wait <name> [--selector \".selector\"] [--visible]")
	fmt.Println("  \t  cdp wait-visible <name> \".selector\"")
	fmt.Println("  \t  cdp click <name> \".selector\" [--has-text REGEX] [--att-value REGEX] [--count N] [--submit-wait-ms N]")
	fmt.Println("  \t  cdp hover <name> \".selector\" [--has-text REGEX] [--att-value REGEX] [--hold DURATION]")
	fmt.Println("  \t  cdp drag <name> \".from\" \".to\" [--from-index N] [--to-index N] [--delay DURATION]")
	fmt.Println("  \t  cdp gesture <name> \".selector\" \"x1,y1 x2,y2 ...\" [--delay DURATION]  (draw, swipe, slide, trace)")
	fmt.Println("  \t  cdp key <name> KEYS [--element \".selector\"] [--cdp]")
	fmt.Println("  \t  cdp scroll <name> <yPx> [--x <xPx>] [--element \".selector\"] [--emit]")
	fmt.Println("  \t  cdp type <name> \".selector\" \"text\" [--has-text REGEX] [--att-value REGEX] [--append]")
	fmt.Println("  \t  cdp upload <name> \"input[type=file]\" <file1> [file2 ...] [--wait]")
	fmt.Println("  \t  cdp inject <name> [--force]")
	fmt.Println("  \t  cdp dom <name> \"CSS selector\"")
	fmt.Println("  \t  cdp styles <name> \"CSS selector\"")
	fmt.Println("  \t  cdp rect <name> \"CSS selector\"")
	fmt.Println("  \t  cdp screenshot <name> [--selector \".composer\"] [--output file.png] [--full-page] [--cdp-clip]")
	fmt.Println("  \t  cdp log <name> [\"setup script\"] [--level REGEX] [--limit N] [--timeout DURATION]")
	fmt.Println("  \t  cdp network-log <name> [--dir PATH] [--url REGEX] [--method REGEX] [--status REGEX] [--mime REGEX]")
	fmt.Println("  \t  cdp keep-alive <name>")
	fmt.Println("  \t  cdp tabs list [--host 127.0.0.1 --port 9222] [--plain]")
	fmt.Println("  \t  cdp tabs open <url> [--host 127.0.0.1 --port 9222] [--activate=false]")
	fmt.Println("  \t  cdp tabs switch <index|id|pattern> [--host 127.0.0.1 --port 9222]")
	fmt.Println("  \t  cdp tabs close <index|id|pattern> [--host 127.0.0.1 --port 9222]")
	fmt.Println("  \t  cdp targets")
	fmt.Println("  cdp disconnect <name>")
	fmt.Println()
	if port, ok := envDefaultPort(); ok {
		fmt.Printf("Configured default port (CDP_PORT): %d\n\n", port)
	}
	fmt.Println("Run 'cdp <command> --help' for command-specific usage.")
}
