package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	//"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	GCOV_PATH = "/spot/dev/3rdParty/cpp/gnu/gcc/gcc-4.7.3/bin/gcov"
	GCOV_FLAGS = "-rlo"
)

func gcov(sourceFile string, graphFile string) error {
	fmt.Printf("%s %s %s %s\n", GCOV_PATH, sourceFile, GCOV_FLAGS, graphFile)
	command := exec.Command(GCOV_PATH, sourceFile, GCOV_FLAGS, graphFile)
	_, err := command.Output()
	if err != nil {
		fmt.Println("error running gcov: %v", err)
	}
	return err
}

func generateCoverage(graphFiles map[string]string) {
	//var waitGroup sync.WaitGroup
	//waitGroup.Add(len(sourceFiles))
	for sourceFile, graphFile := range graphFiles {
		//go func() {
			_ = gcov(sourceFile, graphFile)
			//waitGroup.Done()
		//}()
	}
	//waitGroup.Wait()
}

type GcovLine struct {
	executable bool
	executionCount int
	lineNumber int
	sourceLine string
}

func getExecutionCount(token string) (executable bool, executionCount int) {
	trimToken := strings.TrimSpace(token)
	executionCount, err := strconv.Atoi(trimToken)
	switch {
	case err == nil:
		return true, executionCount
	case trimToken == "#####":
		return true, 0
	case trimToken == "-":
		return false, 0
	}
	return false, 0
}

func parseGcovLine(line string) GcovLine {
	tokens := strings.Split(line, ":")
	if len(tokens) < 3 {
		//TODO: return error
	}
	executable, executionCount := getExecutionCount(tokens[0])
	lineNumber, err := strconv.Atoi(strings.TrimSpace(tokens[1]))
	if err != nil {
		//TODO: return error invalid line number
		fmt.Printf("error: invalid line number %s\n", tokens[1])
	}
	sourceLine := strings.Join(tokens[2:], ":")

	return GcovLine{executable, executionCount, lineNumber, sourceLine}
}

func parseGcovFile(coverageFile string) []GcovLine {
	var gcovLines []GcovLine
	f, err := os.Open(coverageFile)
	if err != nil {
		//TODO: return error
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		gcovLines = append(gcovLines, parseGcovLine(scanner.Text()))
	}
	return gcovLines
}

type GcovFile struct {
	fileName string
	gcovLines []GcovLine
}

func findMatchingFiles(dir string, f func(string) bool) []string {
	var matchingFiles []string
	files, _ := ioutil.ReadDir(dir)
	for _, file := range files {
		if !file.IsDir() && f(file.Name()) {
			matchingFiles = append(matchingFiles, file.Name())
		}
	}
	return matchingFiles
}

func parseCoverage(sourceFiles []string) []GcovFile {
	var mutex sync.Mutex
	var gcovFiles []GcovFile
	var waitGroup sync.WaitGroup
	waitGroup.Add(len(sourceFiles))
	for _, file := range sourceFiles {
		_, sourceFile := filepath.Split(file)
		go func() {
			match := func(name string) bool {
				return strings.HasPrefix(name, sourceFile) && filepath.Ext(name) == ".gcov"
			}
			matchingFiles := findMatchingFiles(".", match)
			if len(matchingFiles) > 0 {
				for _, matchingFile := range matchingFiles {
					mutex.Lock()
					gcovFiles = append(gcovFiles, GcovFile{matchingFile, parseGcovFile(matchingFile)})
					mutex.Unlock()
				}
			} else {
				fmt.Println("error: no gcov file for: " + sourceFile)
			}
			waitGroup.Done()
		}()
	}
	waitGroup.Wait()
	return gcovFiles
}

func getSourceFiles(sourcePath string) []string {
  var sourceFiles []string
	walk := func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(path) == ".cpp" {
			fmt.Println("adding source file: " + path)
			sourceFiles = append(sourceFiles, path)
		}
		return nil
	}
	filepath.Walk(sourcePath, walk)
  return sourceFiles
}

func getFilenameWithoutExt(path string) string {
	_, f := filepath.Split(path)
	return strings.TrimSuffix(f, filepath.Ext(f))
}

func getGraphFiles(sourceFiles []string) map[string]string {
	graphFiles := make(map[string]string)
	for _, sourceFile := range sourceFiles {
		graphFileSuffix := "Tests-" + getFilenameWithoutExt(sourceFile) + ".gcno"
		match := func(file string) bool {
			return strings.HasSuffix(file, graphFileSuffix)
		}
		matchingFiles := findMatchingFiles(".", match)
		if len(matchingFiles) == 1 {
			fmt.Println("adding graph file: " + matchingFiles[0])
			graphFiles[sourceFile] = matchingFiles[0]
		} else {
			fmt.Println("no graph file for: " + graphFileSuffix)
			//TODO: return error
		}
	}
	return graphFiles
}

func groupCoverageBySource(gcovFiles []GcovFile) map[string][]GcovFile {
	files := make(map[string][]GcovFile)
	for _, gcovFile := range gcovFiles {
		tokens := strings.Split(gcovFile.fileName, "##")
		if len(tokens) == 1 {
			fmt.Println("adding graph file: " + gcovFile.fileName)
			files[gcovFile.fileName] = append(files[gcovFile.fileName], gcovFile)
		} else if len(tokens) == 2 {
			fmt.Println("adding graph file: " + gcovFile.fileName)
			files[tokens[1]] = append(files[tokens[1]], gcovFile)
		} else {
			fmt.Println("error: malformed gcov file: " + gcovFile.fileName)
			//TODO return error
		}
	}
	return files
}

func filesEqualLength(files []GcovFile) bool {
	size := len(files[0].gcovLines)
	for _, file := range files {
		if size != len(file.gcovLines) {
			return false
		}
	}
	return true
}

func mergeLine(i int, files []GcovFile) (bool, bool, int) {
	var executable bool
	var executed bool
	executable = true
	executed = false
	for _, file := range files {
		executable = executable && file.gcovLines[i].executable
		executed = executed || file.gcovLines[i].executionCount > 0
	}

	return executable, executed, files[0].gcovLines[i].lineNumber
}

type LineExecutionReport struct {
	executable bool
	executed bool
	lineNumber int
}

type ExecutionReport struct {
	fileName string
	lines []LineExecutionReport
}

func analyzeCoverage(gcovFiles map[string][]GcovFile) []ExecutionReport {
	var reports []ExecutionReport
	for name, files := range gcovFiles {
		fmt.Printf("analyzing %d files for %s\n", len(files), name)
		if filesEqualLength(files) {
			var report ExecutionReport
			report.fileName = name
			for i := range files[0].gcovLines {
				executable, executed, lineNumber := mergeLine(i, files)
				//if lineNumber > 0 {
				//	report.lines = append(report.lines, LineExecutionReport{executable, executed, lineNumber})
				//}
				report.lines = append(report.lines, LineExecutionReport{executable, executed, lineNumber})
			}
			reports = append(reports, report)
		} else {
			fmt.Println("error: coverage files do not same size: " + files[0].fileName)
			//TODO: return error
		}
	}
	return reports
}

func summarizeReports(reports []ExecutionReport) {
	var totalLines int
	var executableLines int
	var executedLines int
	for _, report := range reports {
		fmt.Printf("%s: ", report.fileName)
		for _, line := range report.lines {
			totalLines++
			if line.executable {
				executableLines++
				if line.executed {
					executedLines++
				} else {
					fmt.Printf("%d,", line.lineNumber)
				}
			}
		}
		fmt.Printf("\n")
	}

	fmt.Printf("Total Lines: %d\n", totalLines)
	fmt.Printf("Executable Lines: %d\n", executableLines)
	fmt.Printf("Executed Lines: %d\n", executedLines)
	fmt.Printf("Execution Percentage: %f\n", float32(executedLines) / float32(executableLines))
}

func main() {
	sourcePath := flag.String("source_path", "src", "source file path")
	flag.Parse()

	sourceFiles := getSourceFiles(*sourcePath)
	graphFiles := getGraphFiles(sourceFiles)
	generateCoverage(graphFiles)
	gcovFiles := parseCoverage(sourceFiles)
	groupedFiles := groupCoverageBySource(gcovFiles)
	reports := analyzeCoverage(groupedFiles)
	summarizeReports(reports)
}
