// Only works on OS X, due to pbcopy and terminal-notifier executables.
package main

import (
    "fmt"
    "github.com/snorredc/shareserver/watcher"
    "labix.org/v2/pipe"
    "net/http"
    "net/url"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
)

type exitCode struct {
    code int
}

var args struct {
    host   string
    port   string
    mounts map[string]string
}

func quote(str string) string {
    return "'" + strings.Replace(str, "'", "'\\''", -1) + "'"
}

func copy(str string) error {
    return pipe.Run(pipe.Line(
        pipe.Print(str),
        pipe.Exec("/usr/bin/pbcopy"),
    ))
}

func showNotification(title, message, execute string) error {
    cmd := exec.Command(
        "terminal-notifier",
        "-title", title,
        "-message", message,
        "-sender", "com.apple.Notes",
        "-sound", "default",
        "-execute", execute,
    )
    return cmd.Run()
}

func usage() {
    fmt.Fprintln(os.Stderr, "Usage:", os.Args[0], "host port path [mount:path ...]")
    panic(exitCode{2})
}

func handleError(err error) {
    fmt.Fprintln(os.Stderr, "error:", err.Error())
    panic(exitCode{1})
}

func handleWarning(err error) {
    fmt.Fprintln(os.Stderr, "warning:", err.Error())
}

func parseArgs() {
    if len(os.Args) < 4 {
        usage()
    }
    args.host = os.Args[1]
    args.port = os.Args[2]
    args.mounts = make(map[string]string, len(os.Args)-3)
    args.mounts["/"] = os.Args[3]

    for _, arg := range os.Args[4:] {
        split := strings.SplitN(arg, ":", 2)
        if len(split) != 2 {
            usage()
        }
        mount := strings.TrimSpace(split[0])
        path := strings.TrimSpace(split[1])

        if !strings.HasPrefix(mount, "/") {
            mount = "/" + mount
        }
        if !strings.HasSuffix(mount, "/") {
            mount += "/"
        }

        if _, ok := args.mounts[mount]; ok {
            fmt.Fprintln(os.Stderr, "Duplicate mount", mount)
            usage()
        }
        args.mounts[mount] = path
    }
}

func handleEvent(event watcher.Event) {
    if strings.HasPrefix(event.Name, ".") {
        return
    }

    u := url.URL{
        Scheme: "http",
        Host:   args.host + ":" + args.port,
        Path:   event.Mount + event.Name,
    }
    ustr := u.String()

    var title, execute string

    dir := args.mounts[event.Mount]
    path := filepath.Join(dir, event.Name)
    stat, err := os.Stat(path)
    if err != nil {
        handleWarning(err)
        return
    }
    if stat.IsDir() {
        dir, err := filepath.Abs(dir)
        if err != nil {
            handleWarning(err)
            return
        }
        d, z, n := quote(dir), quote(event.Name+".zip"), quote(event.Name)
        title = "Click to zip"
        execute = "cd " + d + " && zip -r " + z + " " + n + " >/dev/null"
    } else {
        title = "Copied URL to clipboard"
        execute = "/bin/echo -n " + quote(ustr) + " | /usr/bin/pbcopy"
    }

    if err = copy(ustr); err != nil {
        handleError(err)
        return
    }
    if err = showNotification(title, ustr, execute); err != nil {
        handleError(err)
    }
}

func watchDirs() {
    w, err := watcher.New()
    if err != nil {
        handleError(err)
    }

    for mount, path := range args.mounts {
        if err = w.Watch(path, mount); err != nil {
            handleError(err)
        }
    }

    go func() {
        for {
            select {
            case event := <-w.Events:
                handleEvent(event)
            case err := <-w.Error:
                handleError(err)
            }
        }
    }()
    go w.Run()
}

func startServer() {
    for mount, path := range args.mounts {
        fmt.Println(path, "mounted on", mount)
        handler := http.StripPrefix(mount, http.FileServer(http.Dir(path)))
        http.Handle(mount, handler)
    }

    fmt.Println("Server running at http://" + args.host + ":" + args.port + "/")
    if err := http.ListenAndServe(":"+args.port, nil); err != nil {
        handleError(err)
    }
}

func main() {
    defer func() {
        obj := recover()
        if exit, ok := obj.(exitCode); ok {
            os.Exit(exit.code)
        }
        panic(obj)
    }()

    parseArgs()
    watchDirs()
    startServer()
}
