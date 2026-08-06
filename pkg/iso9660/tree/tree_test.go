package tree

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddAndLookupFile(t *testing.T) {
	root := NewRoot()

	node, err := root.AddFile("docs/readme.txt", []byte("hello"))
	require.NoError(t, err)
	require.Equal(t, "readme.txt", node.Name())
	require.Equal(t, "docs/readme.txt", node.FullPath())
	require.True(t, node.IsPending())

	// Parent directory was created implicitly.
	docs := root.Lookup("docs")
	require.NotNil(t, docs)
	require.True(t, docs.IsDir())

	// Lookup ignores leading slashes and version suffixes.
	require.NotNil(t, root.Lookup("/docs/readme.txt"))
	require.NotNil(t, root.Lookup("docs/readme.txt;1"))

	data, err := node.ReadData(2048)
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), data)
}

func TestAddFileReplacesExisting(t *testing.T) {
	root := NewRoot()

	_, err := root.AddFile("a.txt", []byte("one"))
	require.NoError(t, err)
	_, err = root.AddFile("a.txt", []byte("two"))
	require.NoError(t, err)

	data, err := root.Lookup("a.txt").ReadData(2048)
	require.NoError(t, err)
	require.Equal(t, []byte("two"), data)
	require.Len(t, root.Children(), 1)
}

func TestFileDirectoryConflicts(t *testing.T) {
	root := NewRoot()

	_, err := root.AddFile("a", []byte("x"))
	require.NoError(t, err)

	// A file cannot become a path component.
	_, err = root.AddFile("a/b.txt", []byte("y"))
	require.Error(t, err)

	// A directory cannot be replaced by a file.
	_, err = root.AddDirectory("d")
	require.NoError(t, err)
	_, err = root.AddFile("d", []byte("z"))
	require.Error(t, err)
}

func TestRemove(t *testing.T) {
	root := NewRoot()

	_, err := root.AddFile("dir/sub/file.txt", []byte("x"))
	require.NoError(t, err)

	require.NoError(t, root.Remove("dir/sub/file.txt"))
	require.Nil(t, root.Lookup("dir/sub/file.txt"))
	require.NotNil(t, root.Lookup("dir/sub"))

	// Removing a directory removes its subtree.
	require.NoError(t, root.Remove("dir"))
	require.Nil(t, root.Lookup("dir"))

	require.Error(t, root.Remove("missing"))
	require.Error(t, root.Remove("/"))
}

func TestChildrenSorted(t *testing.T) {
	root := NewRoot()
	for _, name := range []string{"zeta", "alpha", "mid"} {
		_, err := root.AddFile(name, nil)
		require.NoError(t, err)
	}
	children := root.Children()
	require.Equal(t, "alpha", children[0].Name())
	require.Equal(t, "mid", children[1].Name())
	require.Equal(t, "zeta", children[2].Name())
}

func TestDirectoriesBreadthFirst(t *testing.T) {
	root := NewRoot()
	for _, p := range []string{"b/deep", "a", "b", "a/nested/more"} {
		_, err := root.AddDirectory(p)
		require.NoError(t, err)
	}

	dirs := root.Directories()
	var paths []string
	for _, d := range dirs {
		paths = append(paths, d.FullPath())
	}
	require.Equal(t, []string{"", "a", "b", "a/nested", "b/deep", "a/nested/more"}, paths)
}

func TestWalkOrder(t *testing.T) {
	root := NewRoot()
	_, err := root.AddFile("dir/file.txt", []byte("x"))
	require.NoError(t, err)
	_, err = root.AddFile("aaa.txt", []byte("y"))
	require.NoError(t, err)

	var visited []string
	err = root.Walk(func(n *Node) error {
		visited = append(visited, n.FullPath())
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"aaa.txt", "dir", "dir/file.txt"}, visited)
}

func TestStripVersion(t *testing.T) {
	require.Equal(t, "FILE.TXT", StripVersion("FILE.TXT;1"))
	require.Equal(t, "FILE.TXT", StripVersion("FILE.TXT;12"))
	require.Equal(t, "FILE.TXT", StripVersion("FILE.TXT"))
	// A non-numeric suffix after ';' is part of the name.
	require.Equal(t, "A;B", StripVersion("A;B"))
}
