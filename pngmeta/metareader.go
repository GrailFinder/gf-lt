package pngmeta

import (
	"bytes"
	"elefant/models"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"strings"
)

const (
	embType = "tEXt"
)

type PngEmbed struct {
	Key   string
	Value string
}

func (c PngEmbed) GetDecodedValue() (*models.CharCardSpec, error) {
	data, err := base64.StdEncoding.DecodeString(c.Value)
	if err != nil {
		return nil, err
	}
	card := &models.CharCardSpec{}
	if err := json.Unmarshal(data, &card); err != nil {
		return nil, err
	}
	return card, nil
}

func extractChar(fname string) (*PngEmbed, error) {
	data, err := os.ReadFile(fname)
	if err != nil {
		return nil, err
	}
	reader := bytes.NewReader(data)
	pr, err := NewPNGStepReader(reader)
	if err != nil {
		return nil, err
	}
	for {
		step, err := pr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
		}
		if step.Type() != embType {
			if _, err := io.Copy(io.Discard, step); err != nil {
				return nil, err
			}
		} else {
			buf, err := io.ReadAll(step)
			if err != nil {
				return nil, err
			}
			dataInstep := string(buf)
			values := strings.Split(dataInstep, "\x00")
			if len(values) == 2 {
				return &PngEmbed{Key: values[0], Value: values[1]}, nil
			}
		}
		if err := step.Close(); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("failed to find embedded char in png: " + fname)
}

func ReadCard(fname, uname string) (*models.CharCard, error) {
	pe, err := extractChar(fname)
	if err != nil {
		return nil, err
	}
	charSpec, err := pe.GetDecodedValue()
	if err != nil {
		return nil, err
	}
	return charSpec.Simplify(uname, fname), nil
}

func ReadDirCards(dirname, uname string) ([]*models.CharCard, error) {
	files, err := os.ReadDir(dirname)
	if err != nil {
		return nil, err
	}
	resp := []*models.CharCard{}
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".png") {
			continue
		}
		fpath := path.Join(dirname, f.Name())
		cc, err := ReadCard(fpath, uname)
		if err != nil {
			// log err
			return nil, err
			// continue
		}
		resp = append(resp, cc)
	}
	return resp, nil
}
