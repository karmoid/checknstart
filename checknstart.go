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
// checknstart.exe -verbose -localfile c:\tools\dacqloc\DARWINSAV.DB -remotefile c:\tools\dacqphy\DARWINSAV.DB.bak -getrate 640k -putrate 640k -cmd notepad.exe -delay 6 -regkey "HKCU\Volatile Environment\2\test" -sqlcmd c:\windows\system32\cmd.exe -sqlarg "/c copy c:\tools\dacqloc\darwinsav.db"
// checknstart.exe -verbose -localfile c:\tools\dacqloc\DARWINSAV.DB -cmd notepad.exe -delay 60 -regkey "HKCU\Volatile Environment\2\test" -sqlcmd c:\windows\system32\cmd.exe -sqlarg "/c copy c:\tools\dacqloc\darwinsav.db"
// checknstart.exe -verbose -localfile c:\tools\dacqloc\DARWINSAV.DB -cmd notepad.exe -delay 60 -regkey "HKCU\Volatile Environment\1\test" -sqlcmd c:\windows\system32\cmd.exe -sqlarg "/c copy c:\tools\dacqloc\darwinsav.db" -endpoint LFRHQBU400619 -setdefault -user emea\chauffourm -pwd xxxxx
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows/registry"
)

// contextCache : Store specific value to alter the program behaviour
// Like an Args container
type (
	contextCache struct {
		endpoint       *string
		share          *string
		remotename     *string
		localname      *string
		localempty     *string
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
		cmdhandle      *exec.Cmd
	}
)

// Create a new instance of the logger. You can have any number of instances.
var mylog = logrus.New()

// contexte : Hold runtime value (from commande line args)
var contexte contextCache

// Valeur par défaut si le paramètre est non renseigné et setdefault "activé"
const dacqname = "DACQ"
const endpointdefval = "ViewClient_Machine_Name"
const clientnameroot = "Volatile Environment"
const clientnamevar = "CLIENTNAME"
const sharedefval = "kheops"
const remotenamedefval = "\\dacq\\base\\darwinsav.db"
const localemptynamedefval = "c:\\b3s\\dacq\\base vierge\\darwinsav.db"
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
const limitputdefval = "10mb"
const maxversion = 5
const spyLoop = 5
const portCheck = 445
const cleanlog = 10 // Versions number. Could be also Days number
const logFileName = "checknstart"

// Get the files' list to copy
func getFiles(src string) (filesOut []os.FileInfo, errOut error) {
	pattern := filepath.Base(src)
	files, err := ioutil.ReadDir(filepath.Dir(src))
	if err != nil {
		return nil, err
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

// Prepare Command Line Args parsing
func setFlagList(ctx *contextCache) {
	ctx.setdefault = flag.Bool("setdefault", false, "Must be use default value if empty")
	ctx.endpoint = flag.String("endpoint", "", fmt.Sprintf("Physical remote device (versus current VDI) [env %s]", endpointdefval))
	ctx.cmd = flag.String("cmd", "", fmt.Sprintf("Target cmd when ready [%s]", cmddefval))
	ctx.verbose = flag.Bool("verbose", true, "Verbose mode")
	// gestion du backup SQL Anywhere
	ctx.howlong = flag.Int64("delay", 5*60, "Checking delay")

	flag.Parse()
}

// Check args and return error if anything is wrong
func processArgs(ctx *contextCache) (err error) {
	setFlagList(&contexte)

	if *ctx.setdefault {
		if *ctx.endpoint == "" {
			*ctx.endpoint = os.Getenv(endpointdefval)
		}
		if *ctx.cmd == "" {
			*ctx.cmd = cmddefval
		}
	}
	return nil
}

// Put CLIENTNAME key in Volatile Environment.
// DACQ program parse all SubKeys below "Volatile Environment" to look for a CLIENTNAME key/value
//
func setClientName(ctx *contextCache) (bool, error) {
	var err error

	// On regarde si la Racine existe.
	// si elle n'existe pas, on a un gros problème
	k, err := registry.OpenKey(registry.CURRENT_USER, clientnameroot, registry.ALL_ACCESS)
	if err != nil {
		return false, err
	}

	// prépare notre sortie
	defer k.Close()

	// on cherche les clefs numériques qui se trouve sous \\Volatile Environment
	keys, err := k.ReadSubKeyNames(10)
	basekey := ""
	if len(keys) > 0 {
		basekey = clientnameroot + "\\" + keys[0]
		mylog.Printf("clef choisie: %v", basekey)
	}

	if basekey == "" {
		basekey = clientnameroot + "\\1"
		mylog.Printf("clef non existante: %v", basekey)
	}

	// Notre choix est fait !
	k2, err := registry.OpenKey(registry.CURRENT_USER, basekey, registry.ALL_ACCESS)
	if err != nil {
		mylog.Printf("Ouverture de la clef [%s] en erreur", basekey)
		return false, err
	}
	defer k2.Close()

	// mylog.Printf("La clef %v a ete ouverte", basekey)
	// mylog.Println("on stocke", *ctx.endpoint, "dans", basekey+"\\"+clientnamevar)
	err = k2.SetStringValue(clientnamevar, *ctx.endpoint)
	if err != nil {
		return false, err
	}
	return true, nil
}

// Mise en surveillance du process que l'on a démarré + surveillance de la disponibilité SMB (port 445)
func spyProcess(ctx *contextCache) (bool, error) {
	if *ctx.verbose {
		log.Printf("Entering in spymode (loop every %d seconds)", spyLoop)
	}
	for {
		time.Sleep(spyLoop * time.Second)
		if ctx.cmdhandle.ProcessState != nil && ctx.cmdhandle.ProcessState.Exited() {
			return false, nil
		}

		conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", *ctx.endpoint, portCheck))
		if err != nil {
			fmt.Println("Connection error:", err)
			mylog.Printf("tcp checking (connectivity) on SMB %s:%d - Unreachable", *ctx.endpoint, portCheck)
			return true, nil
		}
		mylog.Printf("Connection TCP on %s with port %d successful @ip(%s)", *ctx.endpoint, portCheck, conn.RemoteAddr())
		conn.Close()
	}
}

// Démarrage du programme externe que nous allons surveiller
func startCmd(ctx *contextCache, out chan<- int64) {

	// mylog.Printf("Starting [%s]", *ctx.cmd)
	ctx.cmdhandle = exec.Command(*ctx.cmd)

	os.Setenv(clientnamevar, *ctx.endpoint)
	if os.Getenv(clientnamevar) == "" {
		env := os.Environ()
		env = append(env, fmt.Sprintf("%s=%s", clientnamevar, *ctx.endpoint))
		mylog.Printf("[%s=%s] added to environment", clientnamevar, *ctx.endpoint)
		ctx.cmdhandle.Env = env
	}

	if _, err := setClientName(ctx); err != nil {
		mylog.Printf("setClientName en erreur : %v", err)
		out <- 6
		return
	}

	if err := ctx.cmdhandle.Start(); err != nil {
		mylog.Printf("[%s] not started returns: %v", *ctx.cmd, err)
		out <- 6
		return // 6, err
	}
	mylog.Printf("[%s] started with PID: %d", *ctx.cmd, ctx.cmdhandle.Process.Pid)

	out <- 0

	if err := ctx.cmdhandle.Wait(); err != nil {
		mylog.Printf("[%s] Wait returns: %v", *ctx.cmd, err)
		return // 6, err
	}
	return // 0, nil
}

// On va faire du ménage dans les logs détaillés
func cleanLogs(ctx *contextCache) {
	files, err := getFiles(fmt.Sprintf("%s*.log", logFileName))
	if err != nil {
		mylog.Println("cleanlogs error ! Unable to get log files info.")
		return
	}
	filesToRemove := len(files) - cleanlog
	if filesToRemove > 0 && *ctx.verbose {
		mylog.Printf("Cleaning %d on %d files.", filesToRemove, len(files))
	}
	for idx, file := range files {
		// filesOut = append(filesOut, file)
		// ctx.estimatesize += uint64(file.Size())
		if idx < filesToRemove {
			if *ctx.verbose {
				mylog.Printf("Cleaning log (%d) %s", idx, file.Name())
			}
			os.Remove(file.Name())
		}
	}
}

// On ajoute des information de session dans le log debug
//
func dumpDetailSession() {
	mylog.Printf("Local device [%s]", os.Getenv("COMPUTERNAME"))
	mylog.Printf("Broker: DNS Name[%s] / DomainName[%s]  / GatewayLocation[%s] / RemoteIpAddress[%s] / UserName[%s]",
		os.Getenv("ViewClient_Broker_DNS_Name"),
		os.Getenv("ViewClient_Broker_DomainName"),
		os.Getenv("ViewClient_Broker_GatewayLocation"),
		os.Getenv("ViewClient_Broker_Remote_IP_Address"),
		os.Getenv("ViewClient_Broker_UserName"),
	)
	mylog.Printf("Client: IP Address[%s] / Launch ID[%s]  / Session[%s] / Machine[%s.%s] / UserName[%s]",
		os.Getenv("ViewClient_IP_Address"),
		os.Getenv("ViewClient_Launch_ID"),
		os.Getenv("ViewClient_SessionType"),
		os.Getenv("ViewClient_Machine_Name"),
		os.Getenv("ViewClient_Machine_FQDN"),
		os.Getenv("ViewClient_Username"),
	)
	mylog.Printf("Misc: Protocol[%s] / Client type[%s]  / TimeZone[%s]",
		os.Getenv("ViewClient_Protocol"),
		os.Getenv("ViewClient_Type"),
		os.Getenv("ViewClient_Windows_Timezone"),
	)
}

// VersionNum : Litteral version
const VersionNum = "1.5.0"

// V 1.0 - Initial release - 2017 09 11
// V 1.1 - Ajout de x Versions du fichier avant écrasement
// V 1.2 - On copie à pleine vitesse dans les 2 sens - Correction du File Rotate
// V 1.3 - Correction apportée si les fichiers sont en RO (avant écrasement) - On met un fichier vide en Physique après récupération de la base dégradée. - Ajout de log dans fichier
// V 1.4 - Surveillance de la connexion avec le endpoint. Arrêt de process DACQ.exe si perte de connexion
// V 1.4.1 - Ajout de log détaillé sur les sessions Remote
// V 1.4.2 - Meilleure gestion des Fatal Error (on doit rester en mode surveillance si le DACQ.exe est lancé)
// V 1.5.0 - Simplification : Lancement de la commande et positionnement de CLIENTNAME

func main() {
	fmt.Printf("checknstart - Check and start - C.m. 2018 - V%s\n", VersionNum)
	tag := time.Now().Format("20060102-030405")
	file, err := os.OpenFile(fmt.Sprintf("%s-%s.log", logFileName, tag), os.O_APPEND|os.O_CREATE, 0755) // For read access.
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	// The API for setting attributes is a little different than the package level
	// exported logger. See Godoc.
	mylog.Out = file
	mylog.Printf("Starting program ChecknStart %s", VersionNum)

	// Récupération des arguments de base (Variable d'environnement ou Argument en ligne de commande)
	if err := processArgs(&contexte); err != nil {
		mylog.Println(err)
		os.Exit(1) // User error (Usage)
	}

	if *contexte.verbose {
		dumpDetailSession()
	}

	out := make(chan int64, 1)

	go startCmd(&contexte, out)

	retval := <-out
	// mylog.Printf("[%s] started", *contexte.cmd)
	if retval != 0 {
		mylog.Println("WaitAndLaunch error")
		os.Exit(4)
	}

	close(out)

	action, err := spyProcess(&contexte)
	if err != nil {
		mylog.Printf("spyProcess returns: %v", err)
		os.Exit(7)
	}
	if action {
		mylog.Println("Should stop the process. No Kill mode.")
	}

	cleanLogs(&contexte)
	os.Exit(0)
}
