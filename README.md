# app-loger

A small Go logging package (`applog`) that writes plain, human-readable text
files into a folder tree **you** choose:

```
<RootDir>/<folder>/<yyyy>/<mm>/<dd>/<name>.txt
```

Every line looks like:

```
[02/10/2026 14:03:11] Mobile number: 33333333
```

It has two kinds of log files:

| Kind | Created with | Use it for | File name |
|---|---|---|---|
| **Stream** | `lg.Stream(folder, name)` | General logs: server start/stop, database, cron jobs, warnings, errors | Any name you choose; a new date folder is used automatically each day |
| **Session** | `lg.Session(folder, id)` | One transaction (e.g. a bill payment) across several requests | The transaction ID (e.g. KeySessionID); stays in the day it started |

No external dependencies. Needs Go 1.26+.

---

## 1. Install in another project

```bash
go get github.com/amahdi-q/app-loger/applog
```

If the GitHub repository is **private**, tell Go not to use the public proxy first:

```bash
go env -w GOPRIVATE=github.com/amahdi-q/*
git config --global url."git@github.com:".insteadOf "https://github.com/"
go get github.com/amahdi-q/app-loger/applog
```

**Using a local copy instead** (no GitHub access needed), add this to the other
project's `go.mod`:

```
require github.com/amahdi-q/app-loger v0.0.0
replace github.com/amahdi-q/app-loger => ../app-loger
```

Then import it:

```go
import "github.com/amahdi-q/app-loger/applog"
```

---

## 2. Quick start

```go
package main

import (
	"log"

	"github.com/amahdi-q/app-loger/applog"
)

func main() {
	lg, err := applog.New(applog.Config{
		RootDir: "/var/log/PIEServer", // where all log files go
	})
	if err != nil {
		log.Fatal(err)
	}

	sys, _ := lg.Stream("System", "server")
	sys.Info("Server started on :8080")
	sys.Warn("Config value TIMEOUT missing, using 30s")
	sys.Error("Could not connect to database")
}
```

Result: `/var/log/PIEServer/System/2026/10/02/server.txt`

```
[02/10/2026 14:03:11] Server started on :8080
[02/10/2026 14:03:11] [WARNING] Config value TIMEOUT missing, using 30s
[02/10/2026 14:03:11] [ERROR] Could not connect to database
```

> Create **one** `*applog.Logger` when the program starts and share it everywhere
> (pass it in, or keep it in a package variable). It is safe to use from many
> goroutines and HTTP handlers at the same time.

---

## 3. Configuration

All fields except `RootDir` are optional.

```go
bahrain, _ := time.LoadLocation("Asia/Bahrain")

lg, err := applog.New(applog.Config{
	RootDir:    "/var/log/PIEServer",        // required: top-level folder
	DirLayout:  "{folder}/{yyyy}/{mm}/{dd}", // folder structure under RootDir
	FileExt:    ".txt",                      // file extension
	TimeFormat: "02/01/2006 15:04:05",       // format inside [ ... ]
	Location:   bahrain,                     // time zone for timestamps and date folders
	MinLevel:   applog.LevelInfo,            // skip Debug lines

	// Copy every warning and error from ANY file into one central file:
	ErrorsFolder: "Errors",
	ErrorsFile:   "errors",

	// Hide sensitive values written with Field():
	Mask: func(label, value string) string {
		if label == "Card number" && len(value) > 4 {
			return "************" + value[len(value)-4:]
		}
		return value
	},

	// Called if a line cannot be written (disk full, no permission, ...):
	OnWriteError: func(err error) { log.Println("log write failed:", err) },
})
```

| Field | Default | Notes |
|---|---|---|
| `RootDir` | — (required) | Created automatically if missing. Can be relative (`./logs`) or absolute. |
| `DirLayout` | `{folder}/{yyyy}/{mm}/{dd}` | Tokens: `{folder}` `{yyyy}` `{mm}` `{dd}` `{hh}` |
| `FileExt` | `.txt` | e.g. `.log` |
| `TimeFormat` | `02/01/2006 15:04:05` | Go time layout ([reference](https://pkg.go.dev/time#pkg-constants)) |
| `Location` | `time.Local` | Use the server's business time zone |
| `MinLevel` | `LevelDebug` | `LevelDebug` < `LevelInfo` < `LevelWarn` < `LevelError` |
| `ErrorsFolder` + `ErrorsFile` | off | Both must be set to turn it on |
| `Mask` | none | Only applied to `Field()` values |
| `OnWriteError` | prints to stderr | Logging never panics or stops your program |

### Folder layout examples

| `DirLayout` | Resulting path |
|---|---|
| `{folder}/{yyyy}/{mm}/{dd}` | `PIEServer/STC/2026/10/02/100245.txt` |
| `{yyyy}/{mm}/{dd}/{folder}` | `PIEServer/2026/10/02/STC/100245.txt` |
| `{folder}/{yyyy}-{mm}-{dd}` | `PIEServer/STC/2026-10-02/100245.txt` |
| `{folder}` | `PIEServer/STC/100245.txt` (no date folders) |
| `{folder}/{yyyy}/{mm}/{dd}/{hh}` | `PIEServer/STC/2026/10/02/14/100245.txt` |

### Folder and file names

- `folder` can be nested: `lg.Stream("Services/STC", "health")` → `PIEServer/Services/STC/2026/10/02/health.txt`
- File names can use date tokens: `lg.Stream("System", "server_{yyyy}{mm}{dd}")` → `server_20261002.txt`
- Allowed characters: letters, digits, space, `_` `-` `.` (and `{ }` for tokens).
  Names like `../x` or `a/b` (for file names) are rejected with an error, so a bad
  ID coming from a request can never write outside `RootDir`.

---

## 4. Writing lines

Both `Stream` and `Session` have the same methods:

| Method | Output |
|---|---|
| `Info("msg")` / `Infof("x=%d", 5)` | `[time] msg` |
| `Debug` / `Debugf` | `[time] [DEBUG] msg` |
| `Warn` / `Warnf` | `[time] [WARNING] msg` |
| `Error` / `Errorf` | `[time] [ERROR] msg` |
| `Field("Amount to pay", 50)` | `[time] Amount to pay: 50` |
| `Payload("SOAP request", xml)` | `[time] SOAP request:` followed by the body, indented 4 spaces |
| `Step("Authorize transaction")` | A `=====` section header with the time and title in capitals |

Sessions also have:

| Method | Output |
|---|---|
| `End("success")` | A `-----` footer: `TRANSACTION END - STATUS: SUCCESS` |
| `.ID` | The session ID you passed in |

---

## 5. Transactions (Sessions)

A transaction usually has **several HTTP requests** from the kiosk/partner
(inquiry → authorize → verify/confirm). In **every** handler, call
`lg.Session(service, keySessionID)` with the same ID. They all write into the
same file.

```go
var lg *applog.Logger // created once in main()

func handleInquiry(w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("KeySessionID")
	tx, err := lg.Session("STC", id)
	if err != nil { // invalid ID
		http.Error(w, "bad session id", http.StatusBadRequest)
		return
	}

	tx.Step("Bill inquiry request")
	tx.Info("Bill payment request received")
	tx.Field("Mobile number", r.FormValue("mobile"))

	reqXML := buildSOAP(...)
	tx.Payload("SOAP request to STC", reqXML)

	respXML, err := callSTC(reqXML)
	if err != nil {
		tx.Errorf("STC call failed: %v", err)
		tx.End("failed") // the flow ends on an error
		http.Error(w, "provider error", http.StatusBadGateway)
		return
	}
	tx.Payload("SOAP response from STC", respXML)
	tx.Info("Bill details sent to kiosk")
}

func handleAuthorize(w http.ResponseWriter, r *http.Request) {
	tx, _ := lg.Session("STC", r.FormValue("KeySessionID")) // same file
	tx.Step("Authorize transaction")
	tx.Field("Amount to pay", r.FormValue("amount"))
	// ...
}

func handleConfirm(w http.ResponseWriter, r *http.Request) {
	tx, _ := lg.Session("STC", r.FormValue("KeySessionID"))
	tx.Step("Verify payment and confirm")
	// ...
	tx.End("success")
}
```

The file `PIEServer/STC/2026/10/02/100245.txt` will contain:

```
################################################################################
 Service      : STC
 KeySessionID : 100245
 Started      : 02/10/2026 14:03:11
################################################################################

================================================================================
[02/10/2026 14:03:11] BILL INQUIRY REQUEST
================================================================================
[02/10/2026 14:03:11] Bill payment request received
[02/10/2026 14:03:11] Mobile number: 33333333
[02/10/2026 14:03:11] SOAP request to STC:
    <soap:Envelope>
      <GetBill msisdn="33333333"/>
    </soap:Envelope>
...
================================================================================
[02/10/2026 14:03:40] AUTHORIZE TRANSACTION
================================================================================
[02/10/2026 14:03:40] Amount to pay: 50
...
--------------------------------------------------------------------------------
[02/10/2026 14:05:02] TRANSACTION END - STATUS: SUCCESS
--------------------------------------------------------------------------------
```

How a session finds its file:

- The header is written only the **first** time an ID is seen.
- Transactions that cross midnight stay in the **start day's** folder:
  `Session()` looks for the file in today's folder and then in yesterday's.
  A transaction that continues more than one day later starts a new file in
  the new day's folder.
- Session IDs are per folder: `Session("STC", "1")` and `Session("Zain", "1")`
  are different files.
- The package does **not** create IDs. Your server must give each transaction
  a unique KeySessionID.

---

## 6. Central errors file

With `ErrorsFolder: "Errors", ErrorsFile: "errors"`, every `Warn*` and `Error*`
call from any stream or session is **also** written to
`PIEServer/Errors/2026/10/02/errors.txt`, with where it came from:

```
[02/10/2026 14:03:11] [WARNING] [System/server] Batelco endpoint slow to respond (2.4s)
[02/10/2026 14:07:45] [ERROR] [Zain/100246] Zain SOAP call failed: connection timeout after 30s
```

So you can check one file each day to see every problem on the server, then
open the transaction file it points to.

---

## 7. Recommended setup for a server with many services

```go
// logging.go in your project
package app

import "github.com/amahdi-q/app-loger/applog"

var (
	Log    *applog.Logger
	System *applog.Writer
	DB     *applog.Writer
)

func InitLogging(root string) error {
	var err error
	Log, err = applog.New(applog.Config{
		RootDir:      root, // e.g. from config/env: "/var/log/PIEServer"
		ErrorsFolder: "Errors",
		ErrorsFile:   "errors",
	})
	if err != nil {
		return err
	}
	System, _ = Log.Stream("System", "server")
	DB, _ = Log.Stream("System", "database")
	return nil
}
```

```go
// in a service
tx, err := app.Log.Session("Batelco", keySessionID)
```

Suggested tree:

```
PIEServer/
├── System/yyyy/mm/dd/server.txt        startup, shutdown, config
├── System/yyyy/mm/dd/database.txt      DB connections, slow queries
├── Errors/yyyy/mm/dd/errors.txt        every warning and error (automatic copy)
├── Batelco/yyyy/mm/dd/<KeySessionID>.txt
├── Zain/yyyy/mm/dd/<KeySessionID>.txt
└── STC/yyyy/mm/dd/<KeySessionID>.txt
```

---

## 8. Good to know

- **Each write appends and closes the file.** No file handles stay open, so you
  don't need to close anything, and log files can be moved or zipped while the
  server runs.
- **Concurrent requests are safe.** Each line or block is written in one locked
  write, so lines from different goroutines are never mixed together.
  This lock is per process: don't run two server processes writing to the
  **same** `RootDir`.
- **Old logs are not deleted.** Set up cleanup outside the program, e.g. a cron job:
  ```bash
  # delete transaction/log files older than 90 days, then empty folders
  find /var/log/PIEServer -type f -name '*.txt' -mtime +90 -delete
  find /var/log/PIEServer -type d -empty -delete
  ```
- **Permissions:** folders are created `0755`, files `0644`. The user running the
  server must be able to write to `RootDir`.
- **Sensitive data:** don't log full card numbers, PINs or passwords. Use `Mask`
  for values written with `Field()`. `Info`, `Payload` and the other methods are
  written exactly as given, so remove secrets from SOAP/JSON bodies before
  logging them.

---

## 9. Try the demo

```bash
go run ./cmd/demo
find PIEServer -type f
cat PIEServer/STC/*/*/*/100245.txt
```

The demo walks through one STC bill payment (inquiry → authorize → confirm), a
failed Zain transaction, and general server logs.

## 10. Run the tests

```bash
go test -race ./...
```
