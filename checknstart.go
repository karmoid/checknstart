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
//
// Plus pragmatique et immédiat :
// On va gérer les aller-retour entre les fichiers de bases de données SQL anywhere
// locaux et distants.
// Plus tard on fera évoluer le programme en un utilitaire plus complet/complexe.
//
// Command Line sample
// checknstart.exe -setdefault -user pi -pwd xxxxxx -share wd1to -remotefile autorun.inf
//                 -localfile c:\tools\autorun.inf -cmd sublime
//                 -regkey "HKCU\Volatile Environment\1\test" -delay 10
// checknstart.exe -setdefault -user pi -pwd xxxxxx -share wd1to -remotefile autorun.inf -localfile c:\tools\autorun.inf -cmd sublime -regkey "HKCU\Volatile Environment\1\test" -delay 10
//
// checknstart.exe -verbose -localfile c:\tools\dacqtest\DARWINSAV.DB -remotefile c:\tools\dacqtest\DARWINSAV.DB.bak -getrate 64k -putrate 64k -cmd calc.exe -delay 6 -regkey "HKCU\Volatile Environment\1\test" -sqlcmd c:\windows\system32\cmd.exe -sqlarg "copy c:\tools\dacqtest\darwinsav.db"
// checknstart.exe -verbose -localfile c:\tools\dacqtest\DARWINSAV.DB -remotefile c:\tools\dacqtest\DARWINSAV.DB.bak -getrate 640k -putrate 640k -cmd calc.exe -delay 6 -regkey "HKCU\Volatile Environment\2\test" -sqlcmd c:\windows\system32\cmd.exe -sqlarg "/c copy c:\tools\dacqtest\darwinsav.db"
package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/dustin/go-humanize"
	"github.com/efarrer/iothrottler"
)

// context : Store specific value to alter the program behaviour
// Like an Args container
type (
	context struct {
		endpoint       *string
		share          *string
		remotename     *string
		localname      *string
		cmd            *string
		user           *string
		pwd            *string
		backupcmd      *string
		backupargs     *string
		backupbase     *string
		backupuser     *string
		backuppwd      *string
		waitingfor     *string
		howlong        *int64
		tocancel       *bool
		setdefault     *bool
		temp           string
		limitgetstring *string
		limitget       uint64
		limitputstring *string
		limitput       uint64
		verbose        *bool
		remoteinfo     os.FileInfo
		localinfo      os.FileInfo
		refreshneed    bool
		starttime      time.Time
		endtime        time.Time
	}
)

// contexte : Hold runtime value (from commande line args)
var contexte context

// Valeur par défaut si le paramètre est non renseigné et setdefault "activé"
const dacqname = "DACQ"
const endpointdefval = "ViewClient_Machine_Name"
const sharedefval = "kheops"
const remotenamedefval = "\\dacq\\base\\darwinsav.db"
const localnamedefval = "c:\\b3s\\dacq\\base\\darwinsav.db"
const cmddefval = "c:\\b3s\\dacq\\application\\dacq.exe"
const userdefval = "DACQ"
const pwddefval = "dacq"
const backupcmddefval = "c:\\b3s\\Sybase\\SQL Anywhere 5.0\\win32\\dbbackup.exe"
const backupargsdefval = "-c \"dbn=%s;uid=%s;pwd=%s\" -y -d -q"
const backupbasedefval = "darwinsav"
const backupuserdefval = "dba"
const backuppwddefval = "sql"
const waitingfordefval = "HKLM\\SOFTWARE\\KHEOPS\\KZX\\Initialisation\\DATESAUV"
const limitgetdefval = "10mb"
const limitputdefval = "32k"
const maxversion = 5

// Copy one file at once
// mdate : Date to set at end (Touch file)
// src : Source file to copy
// dst : Destination file
// bwlimit : Bandwith limit in bytes by second
func copyFileContents(mdate time.Time, size int64, src, dst string, bwlimit uint64) (int64, error) {
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
			if *contexte.verbose {
				fmt.Print(" KO\n")
			}
			if !*contexte.verbose {
				fmt.Print(".")
			}
			return
		}
		if *contexte.verbose {
			fmt.Print(" OK\n")
		}
		if !*contexte.verbose {
			fmt.Print(".")
		}
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
		if err2 := os.Chtimes(dst, mdate, mdate); err2 != nil {
			log.Fatal(err2)
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
func getFiles(src string) (filesOut []os.FileInfo, errOut error) {
	pattern := filepath.Base(src)
	files, err := ioutil.ReadDir(filepath.Dir(src))
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
			// fmt.Printf("prise en compte de %s", file.Name())
		}
	}
	return filesOut, nil
}

// Get remote path for file (with Net Use or Not)
func getRemotePath(ctx *context) string {
	// si on veut spécifier un path local (pas net use)
	if *ctx.endpoint == "" && *ctx.share == "" {
		return fmt.Sprintf("%s", *ctx.remotename)
	}
	return fmt.Sprintf("\\\\%s\\%s\\%s", *ctx.endpoint, *ctx.share, *ctx.remotename)
}

// Get Path for tempfiles
func getTempPath(ctx *context) string {
	return ctx.temp
}

// Copy one file to another file
func copyOneFile(ctx *context) (written int64, err error) {
	return copyFileContents(ctx.remoteinfo.ModTime(), ctx.remoteinfo.Size(), getRemotePath(ctx), *ctx.localname, ctx.limitget)
}

// exists returns whether the given file or directory exists or not
func exists(path string) (bool, time.Time, error) {
	finfo, err := os.Stat(path)
	if err == nil {
		return true, finfo.ModTime(), nil
	}
	if os.IsNotExist(err) {
		return false, time.Now(), nil
	}
	return false, time.Now(), err
}

// Erase overstored file (> maxversion copies)
func delete(path string, idx int) error {
	return os.Remove(fmt.Sprintf("%s.%d", path, idx))
}

// Rename localfile using a free slotnumber between 0 - pred(maxversion)
func rename(path string, idx int) error {
	return os.Rename(path, fmt.Sprintf("%s.%d", path, idx))
}

// Will rename old localfile to protect it.
// Will keep MAX_VERSION of the file
func protectLocalFile(ctx *context) error {
	var olderdate = time.Now()
	var idx = -1
	for index := 0; index <= maxversion; index++ {
		if *ctx.verbose {
			log.Printf("step %d/%d for %s", index, maxversion, *ctx.localname)
		}
		if index == maxversion {
			if *ctx.verbose {
				log.Printf("%d versions used. Reusing V%d. Delete file %s.%d", maxversion, idx, *ctx.localname, idx)
			}
			if err := delete(*ctx.localname, idx); err != nil {
				return err
			}
			if *ctx.verbose {
				log.Printf("%d versions used. Reusing V%d. Rename file to %s.%d", maxversion, idx, *ctx.localname, idx)
			}
			if err := rename(*ctx.localname, idx); err != nil {
				return err
			}
			return nil
		}

		filehere, modtime, err := exists(fmt.Sprintf("%s.%d", *ctx.localname, index))
		if err != nil {
			return err
		}
		if filehere {
			if modtime.Before(olderdate) {
				olderdate = modtime
				idx = index
			}
			continue
		}
		log.Printf("%d versions used. Using V%d. Rename file to %s.%d", maxversion, index, *ctx.localname, index)
		if err := rename(*ctx.localname, index); err != nil {
			return err
		}
		return nil
	}
	return nil
}

// No more Wildcard and selection in this Array
// fixedCopy because the Src array is predefined
func fixedCopy(ctx *context) (int64, error) {
	protectLocalFile(ctx)
	ctx.starttime = time.Now()
	defer func() { ctx.endtime = time.Now() }()
	bytes, err := copyOneFile(ctx)
	if err != nil {
		return -1, err
	}
	return bytes, nil
}

// Use Backupcmd to do database backup
func dobackup(ctx *context) error {
	args := *ctx.backupargs
	argslog := *ctx.backupargs
	if args == backupargsdefval {
		args = fmt.Sprintf(backupargsdefval, *ctx.backupbase, *ctx.backupuser, *ctx.backuppwd)
		argslog = fmt.Sprintf(backupargsdefval, *ctx.backupbase, *ctx.backupuser, "***")
	}
	if *ctx.verbose {
		log.Println(*ctx.backupcmd, argslog, getTempPath(ctx))
	}
	cmd := exec.Command(*ctx.backupcmd)
	// for idx, argument := range cmd.Args {
	// 	log.Printf("Avant Args - Arg(%d): [%s]", idx, argument)
	// }
	argslist := strings.Split(fmt.Sprintf("%s %s", args, getTempPath(ctx)), " ")
	for _, argument := range argslist {
		if len(argument) > 0 && argument[0] == '"' {
			argument = argument[1:]
		}
		if len(argument) > 0 && argument[len(argument)-1] == '"' {
			argument = argument[:len(argument)-1]
		}
		cmd.Args = append(cmd.Args, argument)
	}
	// for idx, argument := range cmd.Args {
	// 	log.Printf("Après Args - Arg(%d): [%s]", idx, argument)
	// }
	output, err := cmd.CombinedOutput()
	if err != nil {
		if *ctx.verbose {
			log.Printf("Backup exec error !\n%s", output)
		}
	}
	return err
}

// Do backup Cmd and Copy resulting file
func doBackupNCopy(ctx *context) error {
	ctx.starttime = time.Now()
	fileonly := filepath.Base(*ctx.localname)
	if err := dobackup(ctx); err != nil {
		log.Println("doBackupNCopy error ! Unable to backup file.")
		return err
	}
	finfo, err := getFileSpec(fmt.Sprintf("%s\\%s", getTempPath(ctx), fileonly), "temp", *ctx.verbose)
	if err != nil {
		log.Println("doBackupNCopy error ! Unable to get file info.")
		return err
	}
	written, err := copyFileContents(finfo.ModTime(), finfo.Size(), fmt.Sprintf("%s\\%s", getTempPath(ctx), fileonly), getRemotePath(ctx), ctx.limitput)
	if err != nil {
		log.Println("doBackupNCopy error ! Unable to copy TempFile to remoteFile.")
		return err
	}
	if written != finfo.Size() {
		log.Printf("doBackupNCopy error ! Bytes written different that Bytes to copy: %d != %d", written, finfo.Size())
		return err
	}
	if *contexte.verbose {
		ctx.endtime = time.Now()
		elapsedtime := ctx.endtime.Sub(ctx.starttime)
		seconds := int64(elapsedtime.Seconds())
		if seconds == 0 {
			seconds = 1
		}
		fmt.Printf("between(%v,%v)\n  REPORT Temp To Remote:\n  - Elapsed time: %v\n  - Average bandwith usage: %v/s\n",
			ctx.starttime,
			ctx.endtime,
			elapsedtime,
			humanize.Bytes(uint64(written/seconds)))
	}

	return nil
}

// Use net use windows command to map drive/UNCressource
func mapDrive(address string, user string, pw string, verbose bool) ([]byte, error) {
	exec.Command("c:\\windows\\system32\\net.exe", "use", address, "/delete").Run()
	if verbose {
		log.Println("net use", address, fmt.Sprintf("/user:%s", user), "*******")
	}
	return exec.Command("c:\\windows\\system32\\net.exe", "use", address, fmt.Sprintf("/user:%s", user), pw).CombinedOutput()
}

// Get file info
func getFileSpec(src string, lib string, verbose bool) (os.FileInfo, error) {
	files, err := getFiles(src)
	if err != nil {
		return nil, fmt.Errorf("Can't get %s file info for %s", lib, src)
	}
	if len(files) != 1 {
		return nil, fmt.Errorf("Bad %s file info for %s", lib, src)
	}
	if verbose {
		log.Println("Item:", lib,
			"file:", files[0].Name(),
			"size:", humanize.Bytes(uint64(files[0].Size())),
			"bytesized:", files[0].Size(),
			"modified:", files[0].ModTime())
	}
	return files[0], nil
}

// Check if remote file exist
func remoteFileHere(ctx *context) error {
	if *ctx.share != "" || *ctx.endpoint != "" {
		out, err := mapDrive(fmt.Sprintf("\\\\%s\\%s", *ctx.endpoint, *ctx.share), *ctx.user, *ctx.pwd, *ctx.verbose)
		if err != nil {
			if *ctx.verbose {
				log.Println(string(out))
			}
			return fmt.Errorf("Can't map remote share on \\\\%s\\%s", *ctx.endpoint, *ctx.share)
		}
	}
	finfo, err := getFileSpec(getRemotePath(ctx), "remote", *ctx.verbose)
	ctx.remoteinfo = finfo
	return err
}

// on va comparer les dates des fichiers sources et Destination
func compareFileAge(ctx *context) (bool, error) {
	var ltime, rtime time.Time
	finfo, err := getFileSpec(*ctx.localname, "local", *ctx.verbose)
	if err != nil {
		return false, err
	}
	ctx.localinfo = finfo

	rtime = ctx.remoteinfo.ModTime()
	ltime = ctx.localinfo.ModTime()

	ctx.refreshneed = rtime.After(ltime)
	if ctx.refreshneed {
		if *ctx.verbose {
			log.Printf("File need to be refreshed: %s > %s", rtime, ltime)
		}
	}
	return ctx.refreshneed, nil
}

// Prepare Command Line Args parsing
func setFlagList(ctx *context) {
	ctx.setdefault = flag.Bool("setdefault", false, "Must be use default value if empty")
	ctx.endpoint = flag.String("endpoint", "", fmt.Sprintf("Physical remote device (versus current VDI) [env %s]", endpointdefval))
	ctx.share = flag.String("share", "", fmt.Sprintf("Share name on endpoint [%s]", sharedefval))
	ctx.remotename = flag.String("remotefile", "", fmt.Sprintf("Source filename to check & get (no wildcard) [%s]", remotenamedefval))
	ctx.localname = flag.String("localfile", "", fmt.Sprintf("Target Filename for copy (no wildcard) [%s]", localnamedefval))
	ctx.cmd = flag.String("cmd", "", fmt.Sprintf("Target cmd when ready [%s]", cmddefval))
	ctx.user = flag.String("user", "", fmt.Sprintf("User account to use share on endpoint [%s]", userdefval))
	ctx.pwd = flag.String("pwd", "", "Password account to use share on endpoint [***]")
	ctx.limitgetstring = flag.String("getrate", "", fmt.Sprintf("Download bytes per second limit [%s]", limitgetdefval))
	ctx.limitputstring = flag.String("putrate", "", fmt.Sprintf("Upload bytes per second limit [%s]", limitputdefval))
	ctx.verbose = flag.Bool("verbose", false, "Verbose mode")
	// gestion du backup SQL Anywhere
	ctx.backupcmd = flag.String("sqlcmd", "", fmt.Sprintf("Backup tools full path [%s]", backupcmddefval))
	ctx.backupargs = flag.String("sqlarg", "", "Backup tools source args [dbbackup default args]")
	ctx.backupbase = flag.String("sqlbase", "", fmt.Sprintf("SQL anywhere database name [%s]", backupbasedefval))
	ctx.backupuser = flag.String("sqluser", "", fmt.Sprintf("SQL anywhere user account [%s]", backupuserdefval))
	ctx.backuppwd = flag.String("sqlpwd", "", "SQL anwyhere password account [***]")
	ctx.waitingfor = flag.String("regkey", "", fmt.Sprintf("Registry item to check [%s]", waitingfordefval))
	ctx.howlong = flag.Int64("delay", 5*60, "Checking delay")
	ctx.tocancel = flag.Bool("timeoutko", false, "Timeout is it an option? No by default")

	flag.Parse()
}

// Check args and return error if anything is wrong
func processArgs(ctx *context) (err error) {
	setFlagList(&contexte)

	if isWildcard(*ctx.localname) {
		return fmt.Errorf("Local name can't include wildcard: %s", *ctx.localname)
	}

	if isWildcard(*ctx.remotename) {
		return fmt.Errorf("remote name can't include wildcard: %s", *ctx.remotename)
	}

	if *ctx.setdefault {
		if *ctx.pwd == "" {
			*ctx.pwd = pwddefval
		}
		if *ctx.user == "" {
			*ctx.user = userdefval
		}
		if *ctx.endpoint == "" {
			*ctx.endpoint = os.Getenv(endpointdefval)
		}
		if *ctx.share == "" {
			*ctx.share = sharedefval
		}
		if *ctx.remotename == "" {
			*ctx.remotename = remotenamedefval
		}
		if *ctx.localname == "" {
			*ctx.localname = localnamedefval
		}
		if *ctx.cmd == "" {
			*ctx.cmd = cmddefval
		}
		if *ctx.backupcmd == "" {
			*ctx.backupcmd = backupcmddefval
		}
		if *ctx.backupargs == "" {
			*ctx.backupargs = backupargsdefval
		}
		if *ctx.backupbase == "" {
			*ctx.backupbase = backupbasedefval
		}
		if *ctx.backupuser == "" {
			*ctx.backupuser = backupuserdefval
		}
		if *ctx.backuppwd == "" {
			*ctx.backuppwd = backuppwddefval
		}
		if *ctx.waitingfor == "" {
			*ctx.waitingfor = waitingfordefval
		}
	}
	ctx.temp = os.Getenv("TEMP")
	// pour les limites, il n'y a pas de setdefault à positionner
	if *ctx.limitgetstring == "" {
		*ctx.limitgetstring = limitgetdefval
	}

	if *ctx.limitputstring == "" {
		*ctx.limitputstring = limitputdefval
	}

	ctxlimitget, err := humanize.ParseBytes(*ctx.limitgetstring)
	if err != nil {
		return fmt.Errorf("GetLimit value - Error:%s", err) // handle error
	}
	// fmt.Printf("with ctxlimitget=%s, ctx.limitget=%d", *ctx.limitgetstring, ctx.limitget)
	ctx.limitget = ctxlimitget

	ctxlimitput, err := humanize.ParseBytes(*ctx.limitputstring)
	if err != nil {
		return fmt.Errorf("PutLimit value - Error:%s", err) // handle error
	}
	// fmt.Printf("with ctxlimitput=%s, ctx.limitput=%d", *ctx.limitputstring, ctx.limitput)
	ctx.limitput = ctxlimitput

	if *ctx.verbose {
		fmt.Println("putlimit is", humanize.Bytes(uint64(ctx.limitput)), "by second")
		fmt.Printf("approx. %sit/s.\n\n", strings.ToLower(humanize.Bytes(uint64(ctx.limitput*9))))
		fmt.Println("getlimit is", humanize.Bytes(uint64(ctx.limitget)), "by second")
		fmt.Printf("approx. %sit/s.\n\n", strings.ToLower(humanize.Bytes(uint64(ctx.limitget*9))))
	}
	return nil
}

// Check if Registry Key (regkey args) is modified with current date
func sqlUpdated(ctx *context) (bool, error) {
	var regkey registry.Key
	var err error
	var keyvalue registry.Key
	slices := strings.Split(*ctx.waitingfor, "\\")
	// log.Println("slices:", slices)
	// log.Println("len slices:", len(slices))
	if len(slices) > 2 {
		location := strings.Join(slices[1:len(slices)-1], "\\")
		// log.Println("location:", location)
		switch strings.ToUpper(slices[0]) {
		case "HKCU":
			regkey = registry.CURRENT_USER
		case "HKLM":
			regkey = registry.LOCAL_MACHINE
		default:
			return false, fmt.Errorf("Bad Registry Root (HKCU|HKLM) found [%s]", strings.ToUpper(slices[0]))
		}
		keyvalue, err = registry.OpenKey(regkey, location, registry.QUERY_VALUE)
	} else {
		return false, fmt.Errorf("Bad Registry Path - Need (HKCU|HKLM) then (Root) then (Key) [%s]", *ctx.waitingfor)
	}
	if err != nil {
		return false, err
	}
	defer keyvalue.Close()
	s, _, err := keyvalue.GetStringValue(slices[len(slices)-1])
	if err != nil {
		return false, err
	}
	// log.Println("lu en base de registre:", s)
	// log.Println("comparaison:", time.Now().Local().Format("02/01/2006"))
	return s == time.Now().Local().Format("02/01/2006"), nil
}

// Wait for update and Launch copy if needed
func waitandlaunch(ctx *context) error {
	var remainingsecs = *ctx.howlong
	firstdone, err := sqlUpdated(ctx)
	if err != nil {
		log.Println("error in first sqlUpdated?", err)
		return fmt.Errorf("Unable to get SqlUpdated waitingfor flag [%s]", *ctx.waitingfor)
	}
	if firstdone {
		fmt.Println("Current date is already OK in Registry.")
		if *ctx.tocancel {
			fmt.Println("TimeOut will cancel, so return now.")
			return nil
		}
		fmt.Println("Waiting until timeout.")
	}
	for {
		time.Sleep(1 * time.Second)
		remainingsecs--
		done, err := sqlUpdated(ctx)
		if err != nil {
			log.Println("error in sqlUpdated?", err)
			return fmt.Errorf("Unable to get SqlUpdated waitingfor flag [%s], Remains %d second(s)", *ctx.waitingfor, remainingsecs)
		}
		if done && !firstdone {
			log.Println("Current date is OK in Registry")
			return doBackupNCopy(ctx)
		}
		if *ctx.verbose {
			log.Printf("Sleeping 1 second, remaining %d second(s)", remainingsecs)
		}
		if remainingsecs <= 0 {
			log.Printf("No update in registry. %d second(s) elapsed.", *ctx.howlong)
			if !*ctx.tocancel {
				return doBackupNCopy(ctx)
			}
			return nil
		}
	}
}

// VersionNum : Litteral version
const VersionNum = "1.1"

// V 1.0 - Initial release - 2017 09 11
func main() {
	fmt.Printf("checknstart - Check and start - C.m. 2017 - V%s\n", VersionNum)

	// Récupération des arguments de base (Variable d'environnement ou Argument en ligne de commande)
	if err := processArgs(&contexte); err != nil {
		fmt.Println(err)
		os.Exit(1) // User error (Usage)
	}

	// le fichier distant est il accessible
	if err := remoteFileHere(&contexte); err != nil {
		fmt.Println(err)
		os.Exit(2) // File not found
	}

	if *contexte.verbose {
		log.Println("processing on local device", os.Getenv("COMPUTERNAME"),
			"file comparison versus endpoint", *contexte.endpoint)
	}

	//	A-t-on besoin de récupérer la base de données remote en local
	docopy, err := compareFileAge(&contexte)
	if err != nil {
		fmt.Println(err)
		os.Exit(2) // File not found
	}

	// Si les dates de fichier nous l'impose, nous devrons copier les fichiers
	if docopy {
		bytes, err := fixedCopy(&contexte)
		if err != nil {
			fmt.Println(err)
			os.Exit(3) // Copy error
		}
		elapsedtime := contexte.endtime.Sub(contexte.starttime)
		seconds := int64(elapsedtime.Seconds())
		if seconds == 0 {
			seconds = 1
		}
		if *contexte.verbose {
			fmt.Printf("between(%v,%v)\n  REPORT:\n  - Elapsed time: %v\n  - Average bandwith usage: %v/s\n",
				contexte.starttime,
				contexte.endtime,
				elapsedtime,
				humanize.Bytes(uint64(bytes/seconds)))
		}
		fmt.Println("copy done.")
	} else {
		fmt.Println("no copy needed.")
	}
	fmt.Printf("Starting [%s]", *contexte.cmd)
	exec.Command(*contexte.cmd).Start()
	fmt.Printf("[%s] started", *contexte.cmd)
	if err := waitandlaunch(&contexte); err != nil {
		log.Printf("WaitAndLaunch error:%v", err)
		os.Exit(4)
	}
	os.Exit(0)
}
