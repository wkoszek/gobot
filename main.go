package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"
)

const (
	MaxTarSize = 2 * 1000 * 1000
)

var Q = make(chan *job)

var (
	version = capture("go", "version")
	distenv = capture("go", "tool", "dist", "env")
)

func main() {
	for i := 0; i < 5; i++ {
		go worker(i)
	}
	http.HandleFunc("/info", handleInfo)
	http.Handle("/build/", http.StripPrefix("/build/", http.HandlerFunc(handleBuild)))
	listen := ":" + os.Getenv("PORT")
	if listen == ":" {
		listen = ":9001"
	}
	err := http.ListenAndServe(listen, nil)
	if err != nil {
		panic(err)
	}
}

func handleInfo(w http.ResponseWriter, r *http.Request) {
	println("handleinfo")
	w.Write(version)
	w.Write(distenv)
}

func handleBuild(w http.ResponseWriter, r *http.Request) {
	println("build")
	j := &job{
		pkg:  r.URL.Path,
		tar:  http.MaxBytesReader(w, r.Body, MaxTarSize),
		done: make(chan struct{}),
	}
	Q <- j
	<-j.done
	const httpTooLarge = "http: request body too large"
	if j.err != nil && j.err.Error() == httpTooLarge {
		http.Error(w, httpTooLarge, http.StatusRequestEntityTooLarge)
		return
	}
	if j.err != nil {
		log.Println(j.err)
		http.Error(w, "unprocessable entity", 422)
		w.Write(j.out)
		return
	}
	defer j.bin.Close()
	http.ServeContent(w, r, "", time.Time{}, j.bin)
}

func worker(n int) {
	builddir := "/tmp/" + strconv.Itoa(n)
	for j := range Q {
		if err := extractAndBuild(j, builddir); err != nil {
			j.err = err
		}
		j.done <- struct{}{}
	}
}

func extractAndBuild(j *job, builddir string) error {
	defer os.RemoveAll(builddir)
	if err := os.RemoveAll(builddir); err != nil {
		return err
	}

	if err := os.MkdirAll(builddir, 0777); err != nil {
		return err
	}

	err := ExtractAll(j.tar, builddir, 0)
	if err != nil {
		return err
	}
	j.out, err = gobuild(builddir, j.pkg)
	if err != nil {
		return err
	}
	j.bin, err = os.Open(builddir + "/program")
	return err
}

func gobuild(builddir, pkg string) ([]byte, error) {
	cmd := exec.Command("go", "build", "-o", "program")
	//cmd.Env = append(os.Environ(), "GOPATH="+builddir)
	cmd.Dir = builddir
	return cmd.CombinedOutput()
}

type job struct {
	tar      io.Reader
	pkg      string
	builddir string
	bin      *os.File
	out      []byte
	err      error
	done     chan struct{}
}

func capture(name string, arg ...string) []byte {
	cmd := exec.Command(name, arg...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		panic(err)
	}
	return out
}
