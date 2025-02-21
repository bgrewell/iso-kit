package main

import (
	"crypto/md5"
	"fmt"
	"github.com/bgrewell/iso-kit"
	"github.com/bgrewell/iso-kit/pkg/logging"
	"github.com/bgrewell/iso-kit/pkg/option"
	"github.com/bgrewell/usage"
	"io"
	"os"
)

func generateFileMD5(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	hashBytes := hash.Sum(nil)
	return fmt.Sprintf("%x", hashBytes), nil
}

func main() {

	u := usage.NewUsage(
		usage.WithApplicationName("open_and_save"),
		usage.WithApplicationDescription("open_and_save is a functional testing application that is part of iso-kit and is designed to verify that the open, parse and writing logic of iso-kit is working as expected."),
	)
	help := u.AddBooleanOption("h", "help", false, "Display this help message", "", nil)
	rm := u.AddBooleanOption("rm", "remove-test-file", true, "Remove the test file after running the tests", "", nil)
	input := u.AddArgument(1, "input", "The input ISO file to run the tests against", "")
	parsed := u.Parse()

	if !parsed {
		u.PrintError(fmt.Errorf("failed to parse arguments"))
		os.Exit(1)
	}

	if *help {
		u.PrintUsage()
		os.Exit(0)
	}

	if input == nil || *input == "" {
		u.PrintError(fmt.Errorf("location of the input iso file <input> must be provided"))
		os.Exit(1)
	}

	logger := logging.NewLogger(logging.NewSimpleLogger(os.Stderr, logging.LEVEL_TRACE, true))
	i, err := iso.Open(*input,
		option.WithLogger(logger))
	if err != nil {
		fmt.Printf("Failed to open ISO file: %s\n", err)
		os.Exit(1)
	}

	// Save the ISO file to a random temporary file
	o, err := os.CreateTemp("", "open_and_save_test_*.iso")
	if err != nil {
		fmt.Printf("Failed to create temporary file: %s\n", err)
	}

	if *rm {
		defer os.Remove(o.Name())
	} else {
		fmt.Printf("Temporary file: %s\n", o.Name())
	}

	err = i.Save(o)
	if err != nil {
		fmt.Printf("Failed to save ISO file: %s\n", err)
		os.Exit(1)
	}
	o.Close()

	// Verify that the saved ISO file is the same as the input ISO file
	inputHash, err := generateFileMD5(*input)
	if err != nil {
		fmt.Printf("Failed to generate MD5 hash for input file: %s\n", err)
		os.Exit(1)
	}

	outputHash, err := generateFileMD5(o.Name())
	if err != nil {
		fmt.Printf("Failed to generate MD5 hash for output file: %s\n", err)
		os.Exit(1)
	}

	if inputHash != outputHash {
		fmt.Printf("MD5 hash of input file does not match MD5 hash of output file:\n  Input:  %s\n  Output: %s\n", inputHash, outputHash)
		os.Exit(1)
	}

}
