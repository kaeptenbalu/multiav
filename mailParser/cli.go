package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"runtime"
	"syscall"

	"github.com/gammazero/workerpool"
	_ "github.com/ianlancetaylor/cgosymbolizer"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/diode"
	"github.com/schollz/progressbar/v3"
)

const banner = `

███╗   ███╗ █████╗ ██╗██╗     ██████╗  █████╗ ██████╗ ███████╗███████╗     ██████╗██╗     ██╗
████╗ ████║██╔══██╗██║██║     ██╔══██╗██╔══██╗██╔══██╗██╔════╝██╔════╝    ██╔════╝██║     ██║
██╔████╔██║███████║██║██║     ██████╔╝███████║██████╔╝███████╗█████╗█████╗██║     ██║     ██║
██║╚██╔╝██║██╔══██║██║██║     ██╔═══╝ ██╔══██║██╔══██╗╚════██║██╔══╝╚════╝██║     ██║     ██║
██║ ╚═╝ ██║██║  ██║██║███████╗██║     ██║  ██║██║  ██║███████║███████╗    ╚██████╗███████╗██║
╚═╝     ╚═╝╚═╝  ╚═╝╚═╝╚══════╝╚═╝     ╚═╝  ╚═╝╚═╝  ╚═╝╚══════╝╚══════╝     ╚═════╝╚══════╝╚═╝
                                                                                             
Welcome to the MAILPARSING-CLI alpha version: Thank you so much for your effort and your support!`

const (
	AppName     = "mailparser-cli"
	initCmdHelp = "Extracts Attachements and Embeddings from eml files"
)

var (
	inpath           string
	dpath            string
	workers          int
	loglevel         int
	Logger           zerolog.Logger
	//Parser           *featureExtractor.FeatureExtractor
)

func initCliFlags() {
	flag.IntVar(&workers,
		"w", runtime.GOMAXPROCS(0), "Number of parallel worker, default: Number of cores of host system")
	flag.StringVar(&inpath,
		"i", "", "give input path")
	flag.StringVar(&dpath,
		"o", "outdir", "give destination dir")
	flag.IntVar(&loglevel,
		"v", 0, "Set Log-Level Level 1 - 6")
	flag.Parse()
}


func NewLogger(loglevel int) {

	switch loglevel {
	case 6:
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	case 5:
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case 4:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case 3:
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case 2:
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case 1:
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.PanicLevel)
	}

	output := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}

	output.FormatLevel = func(i interface{}) string {
		return fmt.Sprintf("| %-6s|", i)
	}
	output.FormatMessage = func(i interface{}) string {
		return fmt.Sprintf("%s", i)
	}
	output.FormatFieldName = func(i interface{}) string {
		return fmt.Sprintf("%s:", i)
	}
	output.FormatFieldValue = func(i interface{}) string {
		return fmt.Sprintf("%s", i)
	}
	wr := diode.NewWriter(output, 10000, 10*time.Millisecond, func(missed int) {
		fmt.Printf("Logger Dropped %d messages", missed)
	})
	Logger = zerolog.New(wr).With().Timestamp().Caller().Logger()
}


func main() {
	fmt.Println(banner, "\n")
	initCliFlags()
	
	//: Initilize Logger
	NewLogger(loglevel)

	// Initialize System Environemnt
	//fmt.Println("[+] Initializing System Env")
	var rLimit syscall.Rlimit
	rLimit.Max = 999999
	rLimit.Cur = 999999
	err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		Logger.Error().Err(err).Msg("Cannot increase RLimit, you may encounter 'too many open files' issues")
	}

	//: Initialize ctrl+c handler
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for sig := range c {
			fmt.Println("Exist Signal received. -> ", sig)
			fmt.Println("Executing gracefull shutdown...")
			os.Exit(2)
			// sig is a ^C, handle.
		}
	}()


	//: Read all files from input directory-tree
	var filesToAnalyse []string
	err = filepath.Walk(inpath,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir(){
				filesToAnalyse = append(filesToAnalyse, path)
			}
			return nil
		},
	)
	if err != nil {
		Logger.Error().Err(err).Msg("Cannot read dir.")
		os.Exit(0)
	}
	fmt.Println("[*] All files listed.")


	//: create output directory
	if err := os.MkdirAll(dpath, os.ModePerm); err != nil {
		Logger.Error().Err(err).Msg("Cannot create output directory")
		os.Exit(0)
    }


	//: Creates an analsis non blocking workerpool
	//: for concurrent classification tasks
	wp := workerpool.New(workers)
	ProgBar := progressbar.Default(int64(len(filesToAnalyse)))

	
	//: Process all files in parallel
	for _, fpath := range filesToAnalyse {
		fpath := fpath
		
		wp.Submit(func() {			
			f, err := os.OpenFile(fpath, os.O_RDONLY, 0)
			defer f.Close()
			if os.IsPermission(err) {
				Logger.Error().Err(err).Str("path",fpath).Msg("No permission to read file.")
			}
			if err != nil {
				Logger.Error().Err(err).Str("path",fpath).Msg("Cannot parse file.")
			}
			stat, err := f.Stat()
			if err != nil {
				Logger.Error().Err(err).Str("path",fpath).Msg("Cannot get file information. File broken?")
			}

			var eua = &EmailUnderAnalysis{}
			if err == nil {
				rawdata := make([]byte, stat.Size())
				bufio.NewReader(f).Read(rawdata)

				err = ParseEmail(
					eua,
					rawdata,
				)
			}
			if err != nil {
				Logger.Error().Err(err).Str("path",fpath).Msg("Cannot process file.")
			}
			
			//: write Attachement and embeddings to disk
			for _, carvedFile := range eua.AttachmentsEmbeddings{
				path := filepath.Join(dpath,carvedFile.Details.Hash+carvedFile.Details.RealExtension+".file")
				err := os.WriteFile(path, carvedFile.Details.Data, 0644)
				if err != nil{
					Logger.Error().Err(err).Str("path",path).Msg("Cannot create file.")
				}
			} 

			ProgBar.Add(1)
		})	
	}
		
	wp.StopWait()
	fmt.Println("\nProcessing done. Thx so much!")
}
