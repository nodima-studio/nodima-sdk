package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	runnerpackage "github.com/nodima-studio/nodima-sdk/packagekit"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 {
		printUsage(stderr)
		return 2
	}
	switch arguments[0] {
	case "build-go":
		return runBuildGo(ctx, arguments[1:], stdout, stderr)
	case "assemble":
		return runAssemble(ctx, arguments[1:], stdout, stderr)
	case "archive":
		return runArchive(ctx, arguments[1:], stdout, stderr)
	default:
		printUsage(stderr)
		return 2
	}
}

func runAssemble(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("nodima-package assemble", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifest := flags.String("manifest", "", "path to the package manifest template")
	entrypoint := flags.String("entrypoint", "", "path to a compiled Wasm or JavaScript entrypoint")
	output := flags.String("output", "", "new package directory to create")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "nodima-package: unexpected positional arguments")
		return 2
	}
	pkg, err := runnerpackage.Assemble(ctx, *manifest, *entrypoint, *output)
	if err != nil {
		fmt.Fprintln(stderr, "nodima-package:", err)
		return 1
	}
	fmt.Fprintf(stdout, "assembled %s@%s at %s\n", pkg.Manifest.ID, pkg.Manifest.Version, pkg.Root)
	return 0
}

func runBuildGo(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("nodima-package build-go", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifest := flags.String("manifest", "", "path to the package manifest template")
	source := flags.String("source", "", "Go main package to compile")
	output := flags.String("output", "", "new package directory to create")
	workdir := flags.String("workdir", ".", "Go build working directory")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "nodima-package: unexpected positional arguments")
		return 2
	}

	pkg, err := runnerpackage.BuildGo(ctx, runnerpackage.GoBuildOptions{
		ManifestTemplate: *manifest,
		Source:           *source,
		OutputDirectory:  *output,
		WorkingDirectory: *workdir,
	})
	if err != nil {
		fmt.Fprintln(stderr, "nodima-package:", err)
		return 1
	}

	fmt.Fprintf(
		stdout,
		"built %s@%s at %s\n",
		pkg.Manifest.ID,
		pkg.Manifest.Version,
		pkg.Root,
	)
	return 0
}

func runArchive(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("nodima-package archive", flag.ContinueOnError)
	flags.SetOutput(stderr)
	packageDirectory := flags.String("package", "", "verified package directory")
	output := flags.String("output", "", "new .nodima-runner.zip archive to create")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *packageDirectory == "" || *output == "" {
		fmt.Fprintln(stderr, "nodima-package: -package and -output are required")
		return 2
	}
	if err := runnerpackage.ArchiveDirectory(ctx, *packageDirectory, *output); err != nil {
		fmt.Fprintln(stderr, "nodima-package:", err)
		return 1
	}
	fmt.Fprintf(stdout, "archived runner package at %s\n", *output)
	return 0
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(
		writer,
		"usage: nodima-package build-go -manifest FILE -source PACKAGE -output DIRECTORY [-workdir DIRECTORY]\n       nodima-package assemble -manifest FILE -entrypoint FILE -output DIRECTORY\n       nodima-package archive -package DIRECTORY -output FILE.nodima-runner.zip",
	)
}
