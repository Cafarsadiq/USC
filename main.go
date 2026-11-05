package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// wavDurationMs возвращает длительность PCM WAV в миллисекундах по заголовку.
func wavDurationMs(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}

	var riff [12]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil {
		return 0, err
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return 0, fmt.Errorf("not a WAV file")
	}

	pos := int64(12)
	var byteRate uint32
	for {
		var hdr [8]byte
		if _, err := io.ReadFull(f, hdr[:]); err != nil {
			return 0, fmt.Errorf("data chunk not found")
		}
		pos += 8
		id := string(hdr[0:4])
		size := int64(binary.LittleEndian.Uint32(hdr[4:8]))
		pad := size % 2

		switch id {
		case "fmt ":
			if size < 16 {
				return 0, fmt.Errorf("bad fmt chunk")
			}
			var buf [16]byte
			if _, err := io.ReadFull(f, buf[:]); err != nil {
				return 0, err
			}
			byteRate = binary.LittleEndian.Uint32(buf[8:12])
			pos += 16
			size -= 16
		case "data":
			if byteRate == 0 {
				return 0, fmt.Errorf("data chunk before fmt chunk")
			}
			if remaining := st.Size() - pos; size > remaining {
				size = remaining
			}
			return size * 1000 / int64(byteRate), nil
		}

		skip := size + pad
		if _, err := f.Seek(skip, io.SeekCurrent); err != nil {
			return 0, err
		}
		pos += skip
	}
}

func cleanLine(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	return strings.Join(strings.Fields(s), " ")
}

func shorten(s string, n int) string {
	rs := []rune(s)
	if len(rs) > n {
		return string(rs[:n]) + "…"
	}
	return s
}

type charInfo struct {
	count, texts int
	example      string
}

type row struct {
	r  rune
	ci *charInfo
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run . <папка-сплита>   (например: go run . dev)")
		os.Exit(2)
	}
	dir := os.Args[1]

	txts, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	wavs, err := filepath.Glob(filepath.Join(dir, "*.wav"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(txts) == 0 {
		fmt.Fprintf(os.Stderr, "%s: нет .txt файлов\n", dir)
		os.Exit(1)
	}
	sort.Strings(txts)

	var pairs, noWav, badWav, empty int
	var withCyr, overCyr, garbage, badChars int
	var letters, cyrLetters int
	var totalMs, cyrMs int64
	chars := map[rune]*charInfo{}

	for i, txt := range txts {
		if i > 0 && i%20000 == 0 {
			fmt.Fprintf(os.Stderr, "обработано %d из %d\n", i, len(txts))
		}
		base := strings.TrimSuffix(filepath.Base(txt), filepath.Ext(txt))
		wavPath := filepath.Join(dir, base+".wav")
		if _, err := os.Stat(wavPath); err != nil {
			noWav++
			continue
		}
		raw, err := os.ReadFile(txt)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		ms, err := wavDurationMs(wavPath)
		if err != nil {
			badWav++
			continue
		}
		s := cleanLine(string(raw))
		pairs++
		totalMs += ms
		if s == "" {
			empty++
			continue
		}

		tl, tc := 0, 0
		hasGarbage, hasBad := false, false
		seen := map[rune]bool{}
		for _, r := range s {
			if unicode.IsLetter(r) {
				tl++
				if unicode.Is(unicode.Cyrillic, r) {
					tc++
				}
			}
			if strings.ContainsRune("¬|\\_", r) || unicode.In(r, unicode.So, unicode.Mn, unicode.Me, unicode.Cf, unicode.Co, unicode.Cs) {
				hasGarbage = true
			}
			if !(r == ' ' || r == '\'' || (r >= 'a' && r <= 'z')) {
				hasBad = true
			}
			if !(r == ' ' || (r >= 'a' && r <= 'z')) {
				ci := chars[r]
				if ci == nil {
					ci = &charInfo{example: shorten(s, 50)}
					chars[r] = ci
				}
				ci.count++
				if !seen[r] {
					seen[r] = true
					ci.texts++
				}
			}
		}
		letters += tl
		cyrLetters += tc
		if tc > 0 {
			withCyr++
			cyrMs += ms
		}
		if tl > 0 && float64(tc)/float64(tl) > 0.2 {
			overCyr++
		}
		if hasGarbage {
			garbage++
		}
		if hasBad {
			badChars++
		}
	}

	noTxt := len(wavs) - (pairs + badWav)
	pct := func(a, b int) float64 {
		if b == 0 {
			return 0
		}
		return 100 * float64(a) / float64(b)
	}

	fmt.Printf("Папка: %s\n", dir)
	fmt.Printf("Пар txt+wav: %d\n", pairs)
	fmt.Printf("txt без wav: %d, wav без txt: %d, нечитаемых wav: %d\n", noWav, noTxt, badWav)
	fmt.Printf("Часов: %.2f\n", float64(totalMs)/3_600_000)
	fmt.Printf("Пустых текстов: %d\n", empty)
	fmt.Printf("С кириллицей: %d (%.2f%%), %.2f ч\n", withCyr, pct(withCyr, pairs), float64(cyrMs)/3_600_000)
	fmt.Printf("Больше 20%% кириллицы: %d (%.2f%%)\n", overCyr, pct(overCyr, pairs))
	fmt.Printf("Букв: всего %d, кириллица %d\n", letters, cyrLetters)
	fmt.Printf("Эмодзи и значки: %d\n", garbage)
	fmt.Printf("Текстов с символами вне a-z, апострофа и пробела: %d (%.2f%%)\n", badChars, pct(badChars, pairs))

	var rows []row
	for r, ci := range chars {
		rows = append(rows, row{r, ci})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].ci.count != rows[j].ci.count {
			return rows[i].ci.count > rows[j].ci.count
		}
		return rows[i].r < rows[j].r
	})
	fmt.Printf("\nСимволы вне a-z и пробела: %d разных\n", len(rows))
	fmt.Println("символ  код      всего  текстов  пример")
	for i, x := range rows {
		if i >= 60 {
			fmt.Printf("... ещё %d\n", len(rows)-60)
			break
		}
		fmt.Printf("%-7s U+%04X %7d %8d  %q\n", strconv.QuoteRune(x.r), x.r, x.ci.count, x.ci.texts, x.ci.example)
	}
}
