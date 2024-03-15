package prompt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Prompt struct {
	Files      []string
	Prompt     string
	Extensions []string
}

func NewPrompt(prompt string, extensions []string) *Prompt {
	return &Prompt{
		Prompt:     prompt,
		Extensions: extensions,
	}
}

func (p *Prompt) Compile() (string, error) {
	var result string
	for _, file := range p.Files {
		//log.Println("Reading file:", file)
		fileContent, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		result += fmt.Sprintf("----start %s----\r\n%s\r\n----end %s----\r\n\r\n", file, fileContent, file)
	}
	result += "\r\n"
	result += "\r\n"
	result += p.Prompt + ". Your FINAL prompt response NEEDS to end with <end> but ONLY if it is the final response and no more. Do NOT use <end> if you haven't finished writing a function or text."
	return result, nil
}

func (p *Prompt) AddFile(file string) error {
	if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
		return err
	}
	p.Files = append(p.Files, file)
	return nil
}

func (p *Prompt) AddDirectory(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			ext := filepath.Ext(path)
			if ext == "" {
				return nil
			}
			ext = strings.ToLower(ext[1:])

			for _, allowedExt := range p.Extensions {
				if ext == allowedExt {
					if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
						return err
					}
					p.Files = append(p.Files, path)
					break
				}
			}
		}
		return nil
	})

}
