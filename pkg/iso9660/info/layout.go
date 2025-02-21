package info

import (
	"encoding/json"
	"fmt"
	"github.com/fatih/color"
	"slices"
)

type DescriptorInfo struct {
	DescriptorType    string `json:"descriptor_type"`
	DescriptorVersion int    `json:"descriptor_version"`
	DescriptorOffset  int    `json:"descriptor_offset"`
	DescriptorLength  int    `json:"descriptor_length"`
}

type PathTableInfo struct {
	PathTableSource   string `json:"path_table_source"`
	PathTableOffset   int    `json:"path_table_offset"`
	PathTableLength   int    `json:"path_table_length"`
	PathTableEncoding string `json:"path_table_encoding"`
}

type DirectoryRecordInfo struct {
	DirectoryRecordIdentifier     string `json:"directory_record_identifier"`
	DirectoryRecordSource         string `json:"directory_record_source"`
	DirectoryRecordOffset         int    `json:"directory_record_offset"`
	DirectoryRecordExtentLocation int    `json:"directory_record_extent_location"`
	DirectoryRecordExtentLength   int    `json:"directory_record_extent_length"`
	DirectoryRecordIsDirectory    bool   `json:"directory_record_is_directory"`
}

type DirectoryExtentInfo struct {
	DirectoryExtentIdentifier string `json:"directory_extent_identifier"`
	DirectoryExtentOffset     int    `json:"directory_extent_offset"`
	DirectoryExtentLength     int    `json:"directory_extent_length"`
}

func NewISOLayout() *ISOLayout {
	return &ISOLayout{
		VolumeSetStart:    int(^uint(0) >> 1),
		VolumeSetEnd:      0,
		VolumeDescriptors: make([]*DescriptorInfo, 0),
		PathTables:        make([]*PathTableInfo, 0),
		DirectoryRecords:  make([]*DirectoryRecordInfo, 0),
		DirectoryExtents:  make([]*DirectoryExtentInfo, 0),
	}
}

type ISOLayout struct {
	SystemAreaOffset  int                    `json:"system_area_offset"`
	SystemAreaLength  int                    `json:"system_area_length"`
	BootCatalogSystem string                 `json:"boot_catalog_system"`
	BootCatalogOffset int                    `json:"boot_catalog_offset"`
	BootCatalogLength int                    `json:"boot_catalog_length"`
	VolumeSetStart    int                    `json:"volume_set_start"`
	VolumeSetEnd      int                    `json:"volume_set_end"`
	VolumeDescriptors []*DescriptorInfo      `json:"volume_descriptors"`
	PathTables        []*PathTableInfo       `json:"path_tables"`
	DirectoryRecords  []*DirectoryRecordInfo `json:"directory_records"`
	DirectoryExtents  []*DirectoryExtentInfo `json:"directory_extents"`
}

// AddVolumeDescriptor appends a new Volume Descriptor and keeps the list sorted by DescriptorOffset
func (i *ISOLayout) AddVolumeDescriptor(descriptorType string, descriptorVersion int, descriptorOffset int, descriptorLength int) {
	if descriptorOffset < i.VolumeSetStart {
		i.VolumeSetStart = descriptorOffset
	}
	if descriptorOffset+descriptorLength > i.VolumeSetEnd {
		i.VolumeSetEnd = descriptorOffset + descriptorLength
	}

	i.VolumeDescriptors = append(i.VolumeDescriptors, &DescriptorInfo{
		DescriptorType:    descriptorType,
		DescriptorVersion: descriptorVersion,
		DescriptorOffset:  descriptorOffset,
		DescriptorLength:  descriptorLength,
	})

	// Sort using slices.SortFunc
	slices.SortFunc(i.VolumeDescriptors, func(a, b *DescriptorInfo) int {
		return a.DescriptorOffset - b.DescriptorOffset
	})
}

// AddPathTable appends a new Path Table and keeps the list sorted by PathTableOffset
func (i *ISOLayout) AddPathTable(pathTableSource string, pathTableOffset int, pathTableLength int, pathTableEncoding string) {
	i.PathTables = append(i.PathTables, &PathTableInfo{
		PathTableSource:   pathTableSource,
		PathTableOffset:   pathTableOffset,
		PathTableLength:   pathTableLength,
		PathTableEncoding: pathTableEncoding,
	})

	// Sort using slices.SortFunc
	slices.SortFunc(i.PathTables, func(a, b *PathTableInfo) int {
		return a.PathTableOffset - b.PathTableOffset
	})
}

// AddDirectoryRecord appends a new Directory Record only if it doesn’t already exist
// and keeps the list sorted by DirectoryRecordOffset.
func (i *ISOLayout) AddDirectoryRecord(directoryRecordIdentifier string, directoryRecordSource string, directoryRecordOffset int, directoryRecordExtentLocation int, directoryRecordExtentLength int, directoryRecordIsDirectory bool) {
	// Check if this directory record already exists
	for _, record := range i.DirectoryRecords {
		if record.DirectoryRecordIdentifier == directoryRecordIdentifier &&
			record.DirectoryRecordOffset == directoryRecordOffset &&
			record.DirectoryRecordExtentLocation == directoryRecordExtentLocation &&
			record.DirectoryRecordExtentLength == directoryRecordExtentLength {
			return // Skip adding duplicate entry
		}
	}

	// Add new record
	i.DirectoryRecords = append(i.DirectoryRecords, &DirectoryRecordInfo{
		DirectoryRecordIdentifier:     directoryRecordIdentifier,
		DirectoryRecordSource:         directoryRecordSource,
		DirectoryRecordOffset:         directoryRecordOffset,
		DirectoryRecordExtentLocation: directoryRecordExtentLocation,
		DirectoryRecordExtentLength:   directoryRecordExtentLength,
		DirectoryRecordIsDirectory:    directoryRecordIsDirectory,
	})

	// Sort using slices.SortFunc
	slices.SortFunc(i.DirectoryRecords, func(a, b *DirectoryRecordInfo) int {
		return a.DirectoryRecordOffset - b.DirectoryRecordOffset
	})
}

// AddDirectoryExtent appends a new DirectoryExtent and keeps the list sorted by DirectoryExtentOffset
func (i *ISOLayout) AddDirectoryExtent(directoryExtentIdentifier string, directoryExtentOffset int, directoryExtentLength int) {
	// Check if this directory extent already exists (since SVD and PVD point to the same extent)
	for _, extent := range i.DirectoryExtents {
		if extent.DirectoryExtentIdentifier == directoryExtentIdentifier &&
			extent.DirectoryExtentOffset == directoryExtentOffset &&
			extent.DirectoryExtentLength == directoryExtentLength {
			return // Skip adding duplicate entry
		}
	}

	// Add new extent
	i.DirectoryExtents = append(i.DirectoryExtents, &DirectoryExtentInfo{
		DirectoryExtentIdentifier: directoryExtentIdentifier,
		DirectoryExtentOffset:     directoryExtentOffset,
		DirectoryExtentLength:     directoryExtentLength,
	})

	// Sort using slices.SortFunc
	slices.SortFunc(i.DirectoryExtents, func(a, b *DirectoryExtentInfo) int {
		return a.DirectoryExtentOffset - b.DirectoryExtentOffset
	})
}

// PrettyJSON returns a pretty-printed JSON representation of the ISO layout.
func (i *ISOLayout) PrettyJSON() string {
	data, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error generating JSON: %v", err)
	}
	return string(data)
}

//TODO: This should be changed to not print directly and use a writer or return a string instead to decouple it.

// Print outputs the ISO layout details in the order they occur in the ISO.
// - `verbose` prints all details if true; otherwise, prints a high-level summary.
// - `useColor` controls whether colored output is used.
// - `useHexOffset` prints offsets in hexadecimal if true.
func (i *ISOLayout) Print(verbose bool, useColor bool, useHexOffset bool) {
	type layoutItem struct {
		Offset   int
		Length   int
		Detail   string
		Category string
		IsDir    *bool // Pointer to handle directory indicator coloring
	}

	var items []layoutItem

	// Define color functions (default to no color if useColor is false)
	colorMap := map[string]func(a ...interface{}) string{
		"System Area":       color.New(color.FgBlue, color.Bold).SprintFunc(),
		"Volume Descriptor": color.New(color.FgYellow, color.Bold).SprintFunc(),
		"Directory Record":  color.New(color.FgCyan, color.Bold).SprintFunc(),
		"Path Table":        color.New(color.FgMagenta, color.Bold).SprintFunc(),
		"Directory Extent":  color.New(color.FgGreen, color.Bold).SprintFunc(),
	}

	offsetColor := color.New(color.FgGreen).SprintFunc()
	lengthColor := color.New(color.FgGreen).SprintFunc()
	isDirTrueColor := color.New(color.FgHiYellow).SprintFunc() // Brown/Yellow for true
	isDirFalseColor := color.New(color.FgBlue).SprintFunc()    // Blue for false

	if !useColor {
		for key := range colorMap {
			colorMap[key] = func(a ...interface{}) string { return fmt.Sprint(a...) }
		}
		offsetColor = func(a ...interface{}) string { return fmt.Sprint(a...) }
		lengthColor = func(a ...interface{}) string { return fmt.Sprint(a...) }
		isDirTrueColor = offsetColor
		isDirFalseColor = offsetColor
	}

	// System Area
	items = append(items, layoutItem{
		Offset:   i.SystemAreaOffset,
		Length:   i.SystemAreaLength,
		Detail:   "System Area",
		Category: "System Area",
	})

	// Boot Catalog (if present)
	if i.BootCatalogOffset > 0 {
		items = append(items, layoutItem{
			Offset:   i.BootCatalogOffset,
			Length:   i.BootCatalogLength,
			Detail:   fmt.Sprintf("Boot Catalog - System: %s", i.BootCatalogSystem),
			Category: "Volume Descriptor",
		})
	}

	// Volume Descriptor Set
	for _, vd := range i.VolumeDescriptors {
		items = append(items, layoutItem{
			Offset:   vd.DescriptorOffset,
			Length:   vd.DescriptorLength,
			Detail:   fmt.Sprintf("%s (Version: %d)", vd.DescriptorType, vd.DescriptorVersion),
			Category: "Volume Descriptor",
		})
	}

	// Path Tables
	for _, pt := range i.PathTables {
		items = append(items, layoutItem{
			Offset:   pt.PathTableOffset,
			Length:   pt.PathTableLength,
			Detail:   fmt.Sprintf("%s (Encoding: %s)", pt.PathTableSource, pt.PathTableEncoding),
			Category: "Path Table",
		})
	}

	// Directory Records
	for _, dr := range i.DirectoryRecords {
		isDir := dr.DirectoryRecordIsDirectory // Store isDir flag
		items = append(items, layoutItem{
			Offset:   dr.DirectoryRecordOffset,
			Length:   dr.DirectoryRecordExtentLength,
			Detail:   fmt.Sprintf("%s (Extent Location: %d)", dr.DirectoryRecordIdentifier, dr.DirectoryRecordExtentLocation),
			Category: "Directory Record",
			IsDir:    &isDir,
		})
	}

	// Directory Extents
	for _, de := range i.DirectoryExtents {
		items = append(items, layoutItem{
			Offset:   de.DirectoryExtentOffset,
			Length:   de.DirectoryExtentLength,
			Detail:   fmt.Sprintf("%s", de.DirectoryExtentIdentifier),
			Category: "Directory Extent",
		})
	}

	// Sort all items by Offset
	slices.SortFunc(items, func(a, b layoutItem) int {
		return a.Offset - b.Offset
	})

	// Print header
	fmt.Println(color.New(color.FgCyan, color.Bold).Sprint("\n=== ISO Layout ==="))

	// Fixed width settings
	offsetWidth := 14   // Width for [Offset: x]
	categoryWidth := 20 // Width for category names
	lengthWidth := 12   // Width for [Length: x bytes]

	if useHexOffset {
		offsetWidth = 18 // Width for [Offset: 0x...]
	}

	// Print all sorted items with improved formatting
	for _, item := range items {
		// Format offset as hex if requested
		offsetStr := fmt.Sprintf("Offset: %*d", offsetWidth-8, item.Offset)
		if useHexOffset {
			offsetStr = fmt.Sprintf("Offset: %#*x", offsetWidth-8, item.Offset)
		}

		// Apply color only to "true" or "false"
		isDirStr := ""
		if item.IsDir != nil {
			if *item.IsDir {
				isDirStr = fmt.Sprintf(" (IsDir: %s)", isDirTrueColor("true"))
			} else {
				isDirStr = fmt.Sprintf(" (IsDir: %s)", isDirFalseColor("false"))
			}
		}

		fmt.Printf("[%s] [%s] [%s] %s%s\n",
			offsetColor(offsetStr), // Offset (decimal or hex)
			colorMap[item.Category](fmt.Sprintf("%-*s", categoryWidth, item.Category)), // Category
			lengthColor(fmt.Sprintf("%*s", lengthWidth, formatSize(item.Length))),      // Human-readable length
			item.Detail, // Description
			isDirStr,    // Colored "true"/"false" value
		)
	}

	// Print footer
	fmt.Println(color.New(color.FgCyan, color.Bold).Sprint("=============================="))
}

// formatSize converts a size in bytes to a human-readable format.
func formatSize(size int) string {
	const (
		MB = 1024 * 1024
		GB = MB * 1024
	)

	switch {
	case size >= GB:
		return fmt.Sprintf("%8.2f GB", float64(size)/float64(GB))
	case size >= MB:
		return fmt.Sprintf("%8.2f MB", float64(size)/float64(MB))
	default:
		return fmt.Sprintf("%8d B ", size) // Ensures 'B' aligns with MB/GB
	}
}
