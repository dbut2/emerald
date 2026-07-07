package main

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type objRange struct {
	start, end uint32
	group      string
}

var mapSection = regexp.MustCompile(`^ [^ ].*?\s+0x([0-9a-f]{8,16})\s+0x([0-9a-f]+)\s+(\S+)$`)

func groupName(obj string) string {
	if i := strings.Index(obj, ".a("); i >= 0 {
		return strings.TrimSuffix(filepath.Base(obj[:i+2]), ".a")
	}
	return strings.TrimSuffix(filepath.Base(obj), ".o")
}

func parseMap(path string) ([]objRange, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var ranges []objRange
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	inText := false
	for sc.Scan() {
		line := sc.Text()
		if len(line) > 0 && line[0] != ' ' {
			inText = strings.HasPrefix(line, ".text") || strings.HasPrefix(line, "lib_text")
			continue
		}
		if !inText {
			continue
		}
		m := mapSection.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		addr, err1 := strconv.ParseUint(m[1], 16, 64)
		size, err2 := strconv.ParseUint(m[2], 16, 64)
		obj := m[3]
		if err1 != nil || err2 != nil || size == 0 || addr < 0x08000000 || addr >= 0x0A000000 {
			continue
		}
		if !strings.HasSuffix(obj, ".o") && !strings.Contains(obj, ".a(") {
			continue
		}
		ranges = append(ranges, objRange{uint32(addr), uint32(addr + size), groupName(obj)})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	return ranges, nil
}

func groupOf(ranges []objRange, addr uint32) string {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].start > addr })
	if i > 0 && addr < ranges[i-1].end {
		return ranges[i-1].group
	}
	return ""
}
