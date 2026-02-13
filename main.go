package main

import (
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const usageText = `usage: ght [-h|--help] [-u|--url "<value>"] [-m|--markdown] [-c|--copy]

           Get HTML Title

Arguments:

  -h  --help      ヘルプ情報を表示
  -u  --url       取得するURLを指定
  -m  --markdown  Markdown形式で出力
  -c  --copy      クリップボードにコピー
`

var titlePattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

type stringFlag struct {
	value string
	set   bool
}

func main() {
	exitCode := run(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(exitCode)
}

func run(args []string, stdout, stderr io.Writer) int {
	help, markdown, copyOut, urlArg, positional, err := parseArgs(args)
	if err != nil {
		fmt.Fprint(stderr, usageText)
		return 2
	}

	if help {
		fmt.Fprint(stdout, usageText)
		return 0
	}

	url, err := resolveURL(positional, urlArg)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprint(stderr, usageText)
		return 2
	}

	title, err := fetchTitle(url)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	output := title
	if markdown {
		output = fmt.Sprintf("[%s](%s)", title, url)
	}

	fmt.Fprintln(stdout, output)

	if copyOut {
		if err := copyToClipboard(output); err != nil {
			fmt.Fprintf(stderr, "clipboard copy failed: %v\n", err)
			return 1
		}
	}

	return 0
}

func parseArgs(args []string) (bool, bool, bool, stringFlag, []string, error) {
	var (
		help       bool
		markdown   bool
		copyOut    bool
		urlArg     stringFlag
		positional []string
	)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			help = true
		case arg == "-m" || arg == "--markdown":
			markdown = true
		case arg == "-c" || arg == "--copy":
			copyOut = true
		case arg == "-u" || arg == "--url":
			if i+1 >= len(args) {
				return false, false, false, stringFlag{}, nil, errors.New("missing URL value")
			}
			urlArg = stringFlag{value: args[i+1], set: true}
			i++
		case strings.HasPrefix(arg, "--url="):
			urlArg = stringFlag{value: strings.TrimPrefix(arg, "--url="), set: true}
		case strings.HasPrefix(arg, "-u="):
			urlArg = stringFlag{value: strings.TrimPrefix(arg, "-u="), set: true}
		case strings.HasPrefix(arg, "-") && len(arg) > 1:
			// Short option combinations like -mc.
			for _, r := range arg[1:] {
				switch r {
				case 'h':
					help = true
				case 'm':
					markdown = true
				case 'c':
					copyOut = true
				case 'u':
					if i+1 >= len(args) {
						return false, false, false, stringFlag{}, nil, errors.New("missing URL value")
					}
					urlArg = stringFlag{value: args[i+1], set: true}
					i++
				default:
					return false, false, false, stringFlag{}, nil, errors.New("unknown option")
				}
			}
		default:
			positional = append(positional, arg)
		}
	}

	return help, markdown, copyOut, urlArg, positional, nil
}

func resolveURL(positional []string, urlArg stringFlag) (string, error) {
	switch {
	case urlArg.set:
		if len(positional) > 0 {
			return "", errors.New("URLは -u/--url か位置引数のどちらか一方で指定してください")
		}
		if strings.TrimSpace(urlArg.value) == "" {
			return "", errors.New("URLが空です")
		}
		return urlArg.value, nil
	case len(positional) == 1:
		if strings.TrimSpace(positional[0]) == "" {
			return "", errors.New("URLが空です")
		}
		return positional[0], nil
	case len(positional) > 1:
		return "", errors.New("URLは1つだけ指定してください")
	default:
		return "", errors.New("URLを指定してください")
	}
}

func fetchTitle(rawURL string) (string, error) {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", fmt.Errorf("URLの取得に失敗しました: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTPエラー: %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", fmt.Errorf("レスポンスの読み取りに失敗しました: %w", err)
	}

	match := titlePattern.FindSubmatch(body)
	if len(match) < 2 {
		return "", errors.New("titleタグが見つかりませんでした")
	}

	title := html.UnescapeString(string(match[1]))
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return "", errors.New("titleが空でした")
	}

	return title, nil
}

func copyToClipboard(text string) error {
	candidates := clipboardCommands()
	for _, cmd := range candidates {
		if err := pipeToCommand(text, cmd.name, cmd.args...); err == nil {
			return nil
		}
	}
	return errors.New("supported clipboard command not found (pbcopy/xclip/xsel/wl-copy/clip)")
}

type clipboardCmd struct {
	name string
	args []string
}

func clipboardCommands() []clipboardCmd {
	switch runtime.GOOS {
	case "darwin":
		return []clipboardCmd{{name: "pbcopy"}}
	case "windows":
		return []clipboardCmd{{name: "clip"}}
	default:
		return []clipboardCmd{
			{name: "wl-copy"},
			{name: "xclip", args: []string{"-selection", "clipboard"}},
			{name: "xsel", args: []string{"--clipboard", "--input"}},
		}
	}
}

func pipeToCommand(input, name string, args ...string) error {
	if _, err := exec.LookPath(name); err != nil {
		return err
	}
	cmd := exec.Command(name, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if _, err := io.WriteString(stdin, input); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return err
	}
	if err := stdin.Close(); err != nil {
		_ = cmd.Wait()
		return err
	}
	return cmd.Wait()
}
