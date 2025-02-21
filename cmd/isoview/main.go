package main

import (
	"fmt"
	"github.com/bgrewell/iso-kit"
	"github.com/bgrewell/iso-kit/pkg/version"
	"github.com/bgrewell/usage"
	"os"
)

// DisplayISOInfo prints general information about the ISO file.
func DisplayISOInfo(i iso.ISO, verbose bool) {
	//// Retrieve primary volume descriptor (PVD)
	//pvd := i.Pri
	//if pvd == nil {
	//	u.PrintError(fmt.Errorf("failed to retrieve primary volume descriptor"))
	//	return
	//}
	//
	//// Retrieve file system entries
	//entries, err := i.BuildFileSystemEntries(i.RootDirectory, true) // Rock Ridge enabled
	//if err != nil {
	//	u.PrintError(fmt.Errorf("failed to parse file system entries: %w", err))
	//	return
	//}
	//
	//// Count files and directories
	//fileCount, dirCount, symlinkCount := 0, 0, 0
	//totalSize := uint64(0)
	//
	//for _, entry := range entries {
	//	if entry.IsDir {
	//		dirCount++
	//	} else {
	//		fileCount++
	//		totalSize += uint64(entry.Size)
	//	}
	//
	//	// Count symbolic links if Rock Ridge is enabled
	//	if entry.Mode&os.ModeSymlink != 0 {
	//		symlinkCount++
	//	}
	//}

	// Counters
	rrEnabled := 0
	symlinks := 0
	totalSize := uint64(0)

	// Get file system entries
	files, err := i.ListFiles()
	if err != nil {
		fmt.Println("Failed to list files:", err)
	}

	dirs, err := i.ListDirectories()
	if err != nil {
		fmt.Println("Failed to list directories:", err)
	}

	for _, entry := range append(files, dirs...) {
		if entry.HasRockRidge {
			rrEnabled++
		}
		if entry.DirectoryRecord().RockRidge != nil && entry.DirectoryRecord().RockRidge.SymlinkTarget != nil {
			symlinks++
		}
		if !entry.IsDir {
			totalSize += uint64(entry.Size)
		}
	}

	// Print Basic Information
	fmt.Println("=== ISO Information ===")
	if i.GetVolumeID() != "" {
		fmt.Printf("Volume Name: %s\n", i.GetVolumeID())
	}
	if i.GetApplicationID() != "" {
		fmt.Printf("Created By: %s\n", i.GetApplicationID())
	}
	if i.GetDataPreparerID() != "" {
		fmt.Printf("Preparer: %s\n", i.GetDataPreparerID())
	}
	if i.GetPublisherID() != "" {
		fmt.Printf("Publisher: %s\n", i.GetPublisherID())
	}

	fmt.Printf("Volume Size: %d sectors\n", i.GetVolumeSize())
	fmt.Printf("Total Files: %d\n", len(files))
	fmt.Printf("Total Directories: %d\n", len(dirs))
	fmt.Printf("Total Size: %d bytes (%.2f MB)\n", totalSize, float64(totalSize)/1024/1024)

	if verbose {
		// Verbose output with additional metadata
		fmt.Println("\n=== Verbose Information ===")
		fmt.Printf("System Identifier: %s\n", i.GetSystemID())
		fmt.Printf("Volume Set Size: %d\n", -1)
		fmt.Printf("Volume Sequence Number: %d\n", -1)
		fmt.Printf("Logical Block Size: %d bytes\n", -1)
		fmt.Printf("Number of Hard Links: %d\n", -1)
		fmt.Printf("Symbolic Links: %d\n", symlinks)
		fmt.Printf("Root Directory Location: %d (LBA)\n", i.RootDirectoryLocation())

		// Rock Ridge Support
		if i.HasRockRidge() {
			fmt.Println("\n--- Rock Ridge Extensions ---")
			fmt.Println("Rock Ridge Enabled: YES")
			fmt.Printf("  Number of Entries with Extended Attributes: %d\n", rrEnabled)
		} else {
			fmt.Println("\nRock Ridge Extensions: NOT PRESENT")
		}

		// El Torito Boot Support
		if i.HasElTorito() {
			fmt.Println("\n--- El Torito Boot Extensions ---")
			fmt.Println("El Torito Boot Support: YES")
			bootEntries, err := i.ListBootEntries()
			if err != nil {
				fmt.Println("Failed to list boot entries:", err)
			}
			fmt.Printf("Number of Boot Entries: %d\n", len(bootEntries))
			for _, entry := range bootEntries {
				fmt.Printf("  Boot Entry: %s\n", entry.Name)
			}
		}
	}

	fmt.Println("=========================")

	// Print the layout info by converting to pretty json and printing that
	layout := i.GetLayout()
	if layout != nil {
		fmt.Println("=== ISO Layout ===")
		layout.Print(true, true, true)
		fmt.Println("=========================")
	} else {
		fmt.Println("Failed to retrieve ISO layout")
	}
}

func main() {

	u := usage.NewUsage(
		usage.WithApplicationVersion(version.Version()),
		usage.WithApplicationBranch(version.Branch()),
		usage.WithApplicationBuildDate(version.Date()),
		usage.WithApplicationCommitHash(version.Revision()),
		usage.WithApplicationName("isoview"),
		usage.WithApplicationDescription("isoview is a command-line tool for inspecting ISO9660 images, including Rock Ridge, Joliet, and El Torito extensions. It provides detailed volume information, lists files and directories, decodes long filenames, and identifies bootable images."),
	)

	help := u.AddBooleanOption("h", "help", false, "Show this help message", "optional", nil)
	verbose := u.AddBooleanOption("v", "verbose", false, "Print verbose output", "", nil)
	path := u.AddArgument(1, "iso-path", "Path to the file within the ISO to read", "")
	parsed := u.Parse()

	if !parsed {
		u.PrintError(fmt.Errorf("failed to parse arguments"))
		os.Exit(1)
	}

	if *help {
		u.PrintUsage()
		os.Exit(0)
	}

	if path == nil || *path == "" {
		u.PrintError(fmt.Errorf("location of the iso file <path> must be provided"))
		os.Exit(1)
	}

	_ = verbose

	i, err := iso.Open(*path)
	if err != nil {
		u.PrintError(err)
		os.Exit(1)
	}

	DisplayISOInfo(i, *verbose)
	_ = i

}
