package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/sync/semaphore"
)

var binPath = flag.String("bin-path", "decompyle3", "path to decompyle3 binary")

func main() {
	flag.Parse()
	if len(flag.Args()) < 2 {
		log.Fatal("Must specify source and destination directories")
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		<-c
		cancel()
	}()

	var (
		maxWorkers = runtime.GOMAXPROCS(0)
		sem        = semaphore.NewWeighted(int64(maxWorkers))
	)

	if err := recursiveRun(ctx, flag.Arg(0), flag.Arg(1), sem); err != nil {
		log.Panicf("Recursive decompilation failed: %v", err)
	}
}

func recursiveRun(ctx context.Context, srcPath, destPath string, sem *semaphore.Weighted) error {
	err := filepath.Walk(srcPath, func(path string, info os.FileInfo, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			log.Printf("Error while walking file tree: %v", err)
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".pyc" {
			return nil
		}

		if err = sem.Acquire(ctx, 1); err != nil {
			return fmt.Errorf("failed to acquire semaphore: %w", err)
		}
		go func(ctx context.Context, sem *semaphore.Weighted) {
			defer sem.Release(1)
			relPath, err := filepath.Rel(srcPath, path)
			if err != nil {
				log.Printf("Failed to get relative path of %s: %v", path, err)
				return
			}
			newPath := filepath.Join(destPath, relPath)
			dirsOnly := strings.Trim(newPath, filepath.Base(newPath))
			if err = os.MkdirAll(dirsOnly, 0775); err != nil {
				log.Printf("Failed to create destination dir %s: %v", dirsOnly, err)
			}
			newExtension := strings.Trim(newPath, filepath.Ext(newPath)) + ".py"
			if err = decompile(ctx, path, newExtension); err != nil {
				log.Print(err)
			}
		}(ctx, sem)

		return nil
	})

	if err != nil {
		return fmt.Errorf("error in filepath walk: %w", err)
	}

	return nil
}

func decompile(ctx context.Context, srcPath, destPath string) error {
	cmd := exec.CommandContext(
		ctx,
		*binPath,
		"-o",
		destPath,
		srcPath,
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to decompile %s: %w", srcPath, err)
	}

	return nil
}
