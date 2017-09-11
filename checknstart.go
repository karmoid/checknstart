// Package principal de ChecknStart
// Cross Compilation on Raspberry
// > set GOOS=linux
// > set GOARCH=arm
// > go build checknstart
//
// Besoins :
// - Exécuter des commandes particulières en fonction de trigger
// - Trigger de temps ou trigger d'état (fichier présent, fichier modifié depuis un certain temps, ....)
// - Enchaîner des commandes les unes aux autres selon Success / Failed
// - Gérer des variables fichier, chemin, age, timestamp, date, compteur, etc...
// - voir si la notion runonce, run, runif est à prendre en compte
// - Dialogue de confirmation Yes/No ou OK simple
// 	- Détails possible sur la session en cours (variable d'environnement Horizon)
package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/efarrer/iothrottler"
	"github.com/fatih/color"
)

// context : Store specific value to alter the program behaviour
// Like an Args container
type (
	context struct {
		src          *string
		dst          *string
		limitstring  *string
		limit        uint64
		verbose      *bool
		flagNoColor  *bool
		estimatesize uint64
		filecount    uint64
		filecopied   uint64
		starttime    time.Time
		endtime      time.Time
	}
)

// contexte : Hold runtime value (from commande line args)
var contexte context

// Copy one file at once
// src : Source file to copy
// dst : Destination file
// bwlimit : Bandwith limit in bytes by second
func copyFileContents(size int64, src, dst string, bwlimit uint64) (written int64, err error) {
	if *contexte.verbose {
		fmt.Printf("%s -> %s (%s)", src, dst, humanize.Bytes(uint64(size)))
	}
	if !*contexte.verbose {
		fmt.Print(".")
	}

	pool := iothrottler.NewIOThrottlerPool(iothrottler.BytesPerSecond * iothrottler.Bandwidth(bwlimit))
	defer pool.ReleasePool()

	file, err := os.Open(src)
	if err != nil {
		// fmt.Println("Error:", err) // handle error
		return 0, err
	}
	defer func() {
		file.Close()
		if err != nil {
			color.Set(color.FgRed)
			if *contexte.verbose {
				fmt.Print(" KO\n")
			}
			if !*contexte.verbose {
				fmt.Print(".")
			}
			color.Unset()
			return
		}
		color.Set(color.FgGreen)
		if *contexte.verbose {
			fmt.Print(" OK\n")
		}
		if !*contexte.verbose {
			fmt.Print(".")
		}
		color.Unset()
	}()

	throttledFile, err := pool.AddReader(file)
	if err != nil {
		// fmt.Println("Error:", err) // handle error
		// handle error
		return 0, err
	}

	out, err := os.Create(dst)
	if err != nil {
		// fmt.Println("Error:", err) // handle error
		return 0, err
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	bytesw, err := io.Copy(out, throttledFile)
	if err != nil {
		return 0, err
	}
	err = out.Sync()
	return bytesw, err
}

// Check if path contains Wildcard characters
func isWildcard(value string) bool {
	return strings.Contains(value, "*") || strings.Contains(value, "?")
}

// Get the files' list to copy
func getFiles(ctx *context) (filesOut []os.FileInfo, errOut error) {
	pattern := filepath.Base(*ctx.src)
	files, err := ioutil.ReadDir(filepath.Dir(*ctx.src))
	if err != nil {
		log.Fatal(err)
	}
	for _, file := range files {
		if res, err := filepath.Match(strings.ToLower(pattern), strings.ToLower(file.Name())); res {
			if err != nil {
				errOut = err
				return
			}
			filesOut = append(filesOut, file)
			ctx.estimatesize += uint64(file.Size())
			// fmt.Printf("prise en compte de %s", file.Name())
		}
	}
	return filesOut, nil
}

// Copy one file to another file
func copyOneFile(ctx *context, file os.FileInfo) (written int64, err error) {
	if isWildcard(*ctx.src) {
		return copyFileContents(file.Size(), filepath.Join(filepath.Dir(*ctx.src), file.Name()), filepath.Join(*ctx.dst, "\\", file.Name()), ctx.limit)
	}
	return copyFileContents(file.Size(), *ctx.src, *ctx.dst, ctx.limit)
}

// No more Wildcard and selection in this Array
// fixedCopy because the Src array is predefined
func fixedCopy(ctx *context, files []os.FileInfo) (written int64, err error) {
	var wholesize int64
	ctx.filecount = uint64(len(files))
	if *ctx.verbose {
		timeremaining := time.Duration(ctx.estimatesize/ctx.limit) * time.Second
		fmt.Printf("Total size: %s\nFiles: %d\nEstimated time: %v\n",
			humanize.Bytes(ctx.estimatesize),
			ctx.filecount,
			timeremaining)
		ctx.starttime = time.Now()
		color.Set(color.FgYellow)
		fmt.Printf("**START** (%v)\n", ctx.starttime)
		color.Unset()
		defer func() { ctx.endtime = time.Now() }()
	}
	for _, file := range files {
		bytesco, err := copyOneFile(ctx, file)
		if err != nil {
			return wholesize, err
		}
		ctx.filecopied++
		wholesize += bytesco
	}
	return wholesize, nil
}

// Check if src is a wildcard expression
// if True, we must have a Path in dst
// Else dst could be Path or File
func genericCopy(ctx *context) (written int64, myerr error) {
	// var files []os.FileInfo
	files, err := getFiles(ctx)
	if err != nil {
		return 0, err
	}
	bytes, err := fixedCopy(ctx, files)
	if *ctx.verbose {
		elapsedtime := ctx.endtime.Sub(ctx.starttime)
		seconds := int64(elapsedtime.Seconds())
		if seconds == 0 {
			seconds = 1
		}
		color.Set(color.FgYellow)
		fmt.Printf("**END** (%v)\n  REPORT:\n  - Elapsed time: %v\n  - Average bandwith usage: %v/s\n  - Files: %d copied on %d\n",
			ctx.endtime,
			elapsedtime,
			humanize.Bytes(uint64(bytes/seconds)),
			ctx.filecopied,
			ctx.filecount)
		color.Unset() // Don't forget to unset
	}
	return bytes, err
}

// Prepare Command Line Args parsing
func setFlagList(ctx *context) {
	ctx.src = flag.String("src", "", "Source file specification")
	ctx.dst = flag.String("dst", "", "Target path")
	ctx.limitstring = flag.String("limit", "32k", "Bytes per second limit (default 32KB/s)")
	ctx.verbose = flag.Bool("verbose", false, "Verbose mode")
	ctx.flagNoColor = flag.Bool("no-color", false, "Disable color output")
	flag.Parse()
}

// Check args and return error if anything is wrong
func processArgs(ctx *context) (err error) {
	required := []string{"src", "dst"}
	setFlagList(&contexte)
	if *ctx.flagNoColor {
		color.NoColor = true // disables colorized output
	}

	ctxlimit, err := humanize.ParseBytes(*ctx.limitstring)
	if err != nil {
		return fmt.Errorf("Limit value - Error:%s", err) // handle error
	}
	ctx.limit = ctxlimit
	seen := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { seen[f.Name] = true })
	for _, req := range required {
		if !seen[req] {
			// or possibly use `log.Fatalf` instead of:
			return fmt.Errorf("missing required -%s argument/flag", req)
		}
	}

	if *ctx.verbose {
		fmt.Println("limit is", humanize.Bytes(uint64(ctx.limit)), "by second")
		fmt.Printf("approx. %sit/s.\n\n", strings.ToLower(humanize.Bytes(uint64(ctx.limit*9))))
	}
	return nil
}

// VersionNum : Litteral version
const VersionNum = "1.0"

const Config = {
	name: "test"
	firstname: "toto"
}

// V 1.0 - Initial release - 2017 09 11
func main() {
	fmt.Printf("checknstart - Check and start - C.m. 2017 - V%s\n", VersionNum)
	// if err := processArgs(&contexte); err != nil {
	// 	color.Set(color.FgRed)
	// 	fmt.Println(err)
	// 	color.Unset()
	// 	os.Exit(2)
	// }

	// copied, err := genericCopy(&contexte)
	// if err != nil {
	// 	color.Set(color.FgRed)
	// 	fmt.Println("\nError:", err) // handle error
	// 	color.Unset()
	// 	os.Exit(1)
	// }
	// color.Set(color.FgGreen)
	// if contexte.filecopied != contexte.filecount {
	// 	color.Set(color.FgRed)
	// }
	// fmt.Printf("\n%d (%s) bytes copied to %d file(s).\n",
	// 	copied,
	// 	humanize.Bytes(uint64(copied)),
	// 	contexte.filecopied)
	// color.Unset()
	os.Exit(0)
}
