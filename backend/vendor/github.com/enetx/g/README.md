<p align="center">
  <img src="https://user-images.githubusercontent.com/65846651/229838021-741ff719-8c99-45f6-88d2-1a32927bd863.png">
</p>

# 🤪 G: Go Crazy, Go G, Go Nuts!
[![Go Reference](https://pkg.go.dev/badge/github.com/enetx/g.svg)](https://pkg.go.dev/github.com/enetx/g)
[![Go Report Card](https://goreportcard.com/badge/github.com/enetx/g)](https://goreportcard.com/report/github.com/enetx/g)
[![Coverage Status](https://coveralls.io/repos/github/enetx/g/badge.svg?branch=main&service=github)](https://coveralls.io/github/enetx/g?branch=main)
[![Go](https://github.com/enetx/g/actions/workflows/go.yml/badge.svg)](https://github.com/enetx/g/actions/workflows/go.yml)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/enetx/g)

Introducing G, the wackiest Go package on the planet, created to make your coding experience an absolute riot! With G, you can forget about dull and monotonous code, we're all about turning the mundane into the insanely hilarious. It's not just a bicycle, it's almost a motorcycle 🤣!

## 🎉 What's in the box?
1. 📖 **Readable syntax**: Boring code is so yesterday! G turns your code into a party by blending seamlessly with Go and making it super clean and laughably maintainable.
2. 🔀 **Encoding and decoding:** Juggling data formats? No problemo! G's got your back with __Base64__, __URL__, __Gzip__, and __Rot13__ support. Encode and decode like a pro!
3. 🔒 **Hashing extravaganza:** Safety first, right? Hash your data with __MD5__, __SHA1__, __SHA256__, or __SHA512__, and enjoy peace of mind while G watches over your bytes.
4. 📁 **File and directory shenanigans:** Create, read, write, and dance through files and directories with G's fun-tastic functions. Trust us, managing files has never been this entertaining.
5. 🌈 **Data type compatibility:** Strings, integers, floats, bytes, slices, maps, you name it! G is the life of the party, mingling with all your favorite data types.
6. 🔧 **Customize and extend:** Need something extra? G is your best buddy, ready to be extended or modified to suit any project.
7. 📚 **Docs & examples:** We're not just about fun and games, we've got detailed documentation and examples that'll have you smiling from ear to ear as you learn the G way.

Take your Go projects to a whole new level of excitement with G! It's time to stop coding like it's a chore and start coding like it's a celebration! 🥳

# Examples

Generate a securely random string.

<table>
<tr>
<th><code>stdlib</code></th>
<th><code>g</code></th>
</tr>
<tr>
<td>

```go
func main() {
	const charset = "abcdefghijklmnopqrstuvwxyz" +
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	length := 10

	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return
	}

	for i, v := range b {
		b[i] = charset[v%byte(len(charset))]
	}

	result := string(b)
	fmt.Println(result)
}
```
</td>
<td>

```go
func main() {
	result := g.NewString().Random(10)
	fmt.Println(result)
}
```
</td>
</tr>
</table>

GetOrDefault returns the value for a key. If the key does not exist, returns the default value
instead. This function is useful when you want to provide a fallback value for keys that may not
be present in the map.

<table>
<tr>
<th><code>stdlib</code></th>
<th><code>g</code></th>
</tr>
<tr>
<td>

```go
func main() {
	md := make(map[int][]int)

	for i := range 5 {
		md[i] = append(md[i], i)
	}

	fmt.Println(md)
}
```
</td>
<td>

```go
func main() {
	md := g.NewMap[int, g.Slice[int]]()

	for i := range 5 {
		md.Set(i, md.Get(i).UnwrapOrDefault().Append(i))
	}

    // or

	for i := range 5 {
		entry := md.Entry(i)
		entry.OrDefault() // Insert an empty slice if missing
		entry.Transform(
			func(s Slice[int]) Slice[int] {
				return s.Append(i) // Append the current index to the slice
			})
	}

	fmt.Println(md)
}
```
</td>
</tr>
</table>

Copy copies the contents of the current directory to the destination directory.

<table>
<tr>
<th><code>stdlib</code></th>
<th><code>g</code></th>
</tr>
<tr>
<td>

```go
func copyDir(src, dest string) error {
	return filepath.Walk(src, func(path string,
		info fs.FileInfo, err error,
	) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(dest, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		return copyFile(path, destPath, info.Mode())
	})
}

func copyFile(src, dest string, mode fs.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	destFile, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, srcFile)

	return err
}

func main() {
	src := "path/to/source/directory"
	dest := "path/to/destination/directory"

	err := copyDir(src, dest)
	if err != nil {
		fmt.Println("Error copying directory:", err)
	} else {
		fmt.Println("Directory copied successfully")
	}
}
```
</td>
<td>

```go
func main() {
	g.NewDir(".").Copy("copy").Unwrap()
}
```
</td>
</tr>
</table>

RandomSample returns a new slice containing a random sample of elements from the original slice.

<table>
<tr>
<th><code>stdlib</code></th>
<th><code>g</code></th>
</tr>
<tr>
<td>

```go
func RandomSample(slice []int, amount int) []int {
	if amount > len(slice) {
		amount = len(slice)
	}

	samples := make([]int, amount)

	for i := 0; i < amount; i++ {
		index, _ := rand.Int(rand.Reader, big.NewInt(int64(len(slice))))
		samples[i] = slice[index.Int64()]
		slice = append(slice[:index.Int64()], slice[index.Int64()+1:]...)
	}

	return samples
}

func main() {
	slice := []int{1, 2, 3, 4, 5, 6}
	samples := RandomSample(slice, 3)
	fmt.Println(samples)
}
```
</td>
<td>

```go
func main() {
	slice := g.SliceOf(1, 2, 3, 4, 5, 6)
	samples := slice.RandomSample(3)
	fmt.Println(samples)
}
```
</td>
</tr>
</table>
