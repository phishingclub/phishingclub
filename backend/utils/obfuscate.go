package utils

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"strings"
)

// ObfuscationConfig controls how the obfuscation behaves
type ObfuscationConfig struct {
	// MinSplits is the minimum number of parts to split strings into
	MinSplits int
	// MaxSplits is the maximum number of parts to split strings into
	MaxSplits int
	// UseNumberSuffix determines if variable names should have number suffixes
	UseNumberSuffix bool
	// MinNumberSuffix is the minimum value for number suffixes
	MinNumberSuffix int
	// MaxNumberSuffix is the maximum value for number suffixes
	MaxNumberSuffix int
	// UseXOR determines if strings should be XOR encrypted
	UseXOR bool
	// MinXORKey is the minimum XOR key value (1-255)
	MinXORKey int
	// MaxXORKey is the maximum XOR key value (1-255)
	MaxXORKey int
}

// DefaultObfuscationConfig returns sensible defaults for obfuscation
func DefaultObfuscationConfig() ObfuscationConfig {
	return ObfuscationConfig{
		MinSplits:       2,
		MaxSplits:       4,
		UseNumberSuffix: true,
		MinNumberSuffix: 0,
		MaxNumberSuffix: 9,
		UseXOR:          true,
		MinXORKey:       1,
		MaxXORKey:       255,
	}
}

// ObfuscateHTML obfuscates HTML content using compression, base64 encoding,
// and random variable names to make it difficult to fingerprint
func ObfuscateHTML(html string, config ObfuscationConfig) (string, error) {
	// generate random variable names to avoid fingerprinting (need early for xor function name)
	varNames := generateRandomVariableNames(11, config)
	xorFuncName := varNames[9]
	windowVar := varNames[10]

	// randomly select method to access window object
	windowAccessor := getRandomWindowAccessor()
	// compress the HTML to reduce size and add another layer
	compressed, err := compressGzip([]byte(html))
	if err != nil {
		return "", fmt.Errorf("failed to compress html: %w", err)
	}

	// encode to base64
	encoded := base64.StdEncoding.EncodeToString(compressed)

	// split the base64 payload to avoid detection
	encodedSplit := splitStringRandom(encoded, config, xorFuncName)

	// split critical strings to avoid detection
	atobSplit := splitStringRandom("atob", config, xorFuncName)
	uint8ArraySplit := splitStringRandom("Uint8Array", config, xorFuncName)
	fromSplit := splitStringRandom("from", config, xorFuncName)
	charCodeAtSplit := splitStringRandom("charCodeAt", config, xorFuncName)
	responseSplit := splitStringRandom("Response", config, xorFuncName)
	bufferSplit := splitStringRandom("buffer", config, xorFuncName)
	bodySplit := splitStringRandom("body", config, xorFuncName)
	pipeThroughSplit := splitStringRandom("pipeThrough", config, xorFuncName)
	decompressionStreamSplit := splitStringRandom("DecompressionStream", config, xorFuncName)
	gzipSplit := splitStringRandom("gzip", config, xorFuncName)
	textSplit := splitStringRandom("text", config, xorFuncName)
	thenSplit := splitStringRandom("then", config, xorFuncName)
	documentSplit := splitStringRandom("document", config, xorFuncName)
	openSplit := splitStringRandom("open", config, xorFuncName)
	writeSplit := splitStringRandom("write", config, xorFuncName)
	closeSplit := splitStringRandom("close", config, xorFuncName)

	// create xor helper function if needed
	xorFunc := ""
	if config.UseXOR {
		// obfuscate the xor function internals
		xorVars := generateRandomVariableNames(4, config)
		// create a minimal config without xor to avoid recursion
		noXorConfig := config
		noXorConfig.UseXOR = false
		fromCharCodeSplit := splitStringRandom("fromCharCode", noXorConfig, "")
		parseIntSplit := splitStringRandom("parseInt", noXorConfig, "")
		substrSplit := splitStringRandom("substr", noXorConfig, "")
		lengthSplit := splitStringRandom("length", noXorConfig, "")

		xorFunc = fmt.Sprintf(`function %s(%s,%s){var %s='';for(var %s=0;%s<%s[%s];%s+=2)%s+=String[%s](%s[%s](%s[%s](%s,2),16)^%s);return %s;}`,
			xorFuncName, xorVars[0], xorVars[1], xorVars[2], xorVars[3],
			xorVars[3], xorVars[0], lengthSplit, xorVars[3], xorVars[2],
			fromCharCodeSplit, windowVar, parseIntSplit, xorVars[0], substrSplit, xorVars[3],
			xorVars[1], xorVars[2])
	}

	// create the deobfuscation script with heavily obfuscated strings (minified)
	deobfScript := fmt.Sprintf(`%svar %s=%s;var %s=%s;var %s=%s[%s](%s);var %s=%s[%s][%s](%s,function(%s){return %s[%s](0);});var %s=new %s[%s](%s[%s])[%s][%s](new %s[%s](%s));var %s=new %s[%s](%s);%s[%s]()[%s](function(%s){%s[%s][%s]();%s[%s][%s](%s);%s[%s][%s]();});`,
		xorFunc,
		windowVar, windowAccessor,
		varNames[0], encodedSplit,
		varNames[1], windowVar, atobSplit, varNames[0],
		varNames[2], windowVar, uint8ArraySplit, fromSplit, varNames[1], varNames[8], varNames[8], charCodeAtSplit,
		varNames[3], windowVar, responseSplit, varNames[2], bufferSplit, bodySplit, pipeThroughSplit, windowVar, decompressionStreamSplit, gzipSplit,
		varNames[4], windowVar, responseSplit, varNames[3],
		varNames[4], textSplit, thenSplit, varNames[5],
		windowVar, documentSplit, openSplit,
		windowVar, documentSplit, writeSplit, varNames[5],
		windowVar, documentSplit, closeSplit)

	// HTML5 template
	template := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
</head>
<body>
<script>%s</script>
</body>
</html>`, deobfScript)

	return template, nil
}

// getRandomWindowAccessor returns a random way to access the window object
func getRandomWindowAccessor() string {
	accessors := []string{
		"self",
		"this",
		"globalThis",
		"Function('return this')()",
		"(function(){return this})()",
		"(0,eval)('this')",
	}

	// randomly select one
	idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(accessors))))
	return accessors[idx.Int64()]
}

// xorString encrypts a string with XOR using the given key and returns hex encoded string
func xorString(s string, key byte) string {
	var result strings.Builder
	for i := 0; i < len(s); i++ {
		result.WriteString(fmt.Sprintf("%02x", s[i]^key))
	}
	return result.String()
}

// splitStringRandom splits a string into random parts and returns a concatenation expression
func splitStringRandom(s string, config ObfuscationConfig, xorFuncName string) string {
	if len(s) <= 1 {
		return fmt.Sprintf(`"%s"`, s)
	}

	// determine number of splits based on config
	minParts := config.MinSplits
	maxParts := config.MaxSplits
	if minParts < 1 {
		minParts = 1
	}
	if maxParts < minParts {
		maxParts = minParts
	}

	rangeSize := maxParts - minParts + 1
	numParts, _ := rand.Int(rand.Reader, big.NewInt(int64(rangeSize)))
	parts := int(numParts.Int64()) + minParts

	if parts > len(s) {
		parts = len(s)
	}

	// generate random split positions
	positions := make([]int, 0, parts-1)
	for i := 0; i < parts-1; i++ {
		maxPos := int64(len(s) - 1)
		if maxPos < 1 {
			break
		}
		pos, _ := rand.Int(rand.Reader, big.NewInt(maxPos))
		positions = append(positions, int(pos.Int64())+1)
	}

	// sort positions to split correctly
	// bubble sort since we have few elements
	for i := 0; i < len(positions); i++ {
		for j := i + 1; j < len(positions); j++ {
			if positions[i] > positions[j] {
				positions[i], positions[j] = positions[j], positions[i]
			}
		}
	}

	// remove duplicates and ensure boundaries
	uniquePositions := make([]int, 0)
	lastPos := 0
	for _, pos := range positions {
		if pos > lastPos && pos < len(s) {
			uniquePositions = append(uniquePositions, pos)
			lastPos = pos
		}
	}

	// build the split string parts with optional XOR encryption
	var result strings.Builder
	start := 0
	for i, pos := range uniquePositions {
		if i > 0 {
			result.WriteString(" + ")
		}
		part := s[start:pos]
		if config.UseXOR {
			// generate random XOR key within configured range
			keyRange := config.MaxXORKey - config.MinXORKey + 1
			if keyRange < 1 {
				keyRange = 1
			}
			xorKey, _ := rand.Int(rand.Reader, big.NewInt(int64(keyRange)))
			key := byte(int(xorKey.Int64()) + config.MinXORKey)

			// xor encrypt the part
			encrypted := xorString(part, key)
			result.WriteString(fmt.Sprintf(`%s("%s",%d)`, xorFuncName, encrypted, key))
		} else {
			result.WriteString(fmt.Sprintf(`"%s"`, part))
		}
		start = pos
	}

	// add the last part
	if len(uniquePositions) > 0 {
		result.WriteString(" + ")
	}
	lastPart := s[start:]
	if config.UseXOR {
		// generate random XOR key for last part
		keyRange := config.MaxXORKey - config.MinXORKey + 1
		if keyRange < 1 {
			keyRange = 1
		}
		xorKey, _ := rand.Int(rand.Reader, big.NewInt(int64(keyRange)))
		key := byte(int(xorKey.Int64()) + config.MinXORKey)

		// xor encrypt the last part
		encrypted := xorString(lastPart, key)
		result.WriteString(fmt.Sprintf(`%s("%s",%d)`, xorFuncName, encrypted, key))
	} else {
		result.WriteString(fmt.Sprintf(`"%s"`, lastPart))
	}

	return result.String()
}

// compressGzip compresses data using gzip
func compressGzip(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, err := writer.Write(data)
	if err != nil {
		writer.Close()
		return nil, err
	}
	err = writer.Close()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// generateRandomVariableNames generates random variable names to prevent fingerprinting
func generateRandomVariableNames(count int, config ObfuscationConfig) []string {
	// common but non-suspicious variable name prefixes
	prefixes := []string{
		"a", "b", "c", "d", "v", "x", "i", "j", "k", "l", "m", "n", "p", "q", "r", "s", "t", "u", "w", "y", "z",
	}

	names := make([]string, count)
	used := make(map[string]bool)

	for i := 0; i < count; i++ {
		for {
			// select random prefix
			prefixIdx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(prefixes))))
			prefix := prefixes[prefixIdx.Int64()]

			var name string
			if config.UseNumberSuffix {
				// generate random suffix within configured range
				suffixRange := config.MaxNumberSuffix - config.MinNumberSuffix + 1
				if suffixRange < 1 {
					suffixRange = 1
				}
				suffix, _ := rand.Int(rand.Reader, big.NewInt(int64(suffixRange)))
				name = fmt.Sprintf("%s%d", prefix, int(suffix.Int64())+config.MinNumberSuffix)
			} else {
				name = prefix
			}

			// ensure uniqueness
			if !used[name] && !isReservedWord(name) {
				names[i] = name
				used[name] = true
				break
			}
		}
	}

	return names
}

// isReservedWord checks if a string is a JavaScript reserved word
func isReservedWord(word string) bool {
	reserved := []string{
		"break", "case", "catch", "class", "const", "continue", "debugger",
		"default", "delete", "do", "else", "export", "extends", "finally",
		"for", "function", "if", "import", "in", "instanceof", "let", "new",
		"return", "super", "switch", "this", "throw", "try", "typeof", "var",
		"void", "while", "with", "yield", "enum", "await", "implements",
		"interface", "package", "private", "protected", "public", "static",
	}

	wordLower := strings.ToLower(word)
	for _, r := range reserved {
		if r == wordLower {
			return true
		}
	}
	return false
}

/*
// decompressGzip decompresses gzip data (for testing purposes)
func decompressGzip(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}
*/
