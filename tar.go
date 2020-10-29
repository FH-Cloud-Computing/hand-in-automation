package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

//Source: https://medium.com/@skdomino/taring-untaring-files-in-go-6b07cf56bc07
func tarDirectory(src string, writer io.Writer) error {
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("inable to tar files - %v", err.Error())
	}

	gz := gzip.NewWriter(writer)

	tw := tar.NewWriter(gz)

	rootPath, err := filepath.Abs(src)
	if err != nil {
		return fmt.Errorf("failed to get absolute path of %s (%v)", src, err)
	}

	err = filepath.Walk(src, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !fi.Mode().IsRegular() {
			return nil
		}

		fileName := file[len(rootPath)+1:]

		header, err := tar.FileInfoHeader(fi, fileName)
		if err != nil {
			return err
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		f, err := os.Open(file)
		if err != nil {
			return err
		}

		if _, err := io.Copy(tw, f); err != nil {
			return err
		}

		f.Close()

		return nil
	})
	tw.Close()
	gz.Close()
	return err
}
