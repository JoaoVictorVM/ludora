package views

import (
	"context"
	"strings"
	"testing"
)

func renderHome(t *testing.T) string {
	t.Helper()

	var sb strings.Builder
	if err := Home(nil, 0, false).Render(context.Background(), &sb); err != nil {
		t.Fatalf("rendering home: %v", err)
	}

	return sb.String()
}

func TestLayout_RendersFooterAttribution(t *testing.T) {
	html := renderHome(t)

	if !strings.Contains(html, "Dados dos jogos fornecidos por") {
		t.Error("footer is missing the RAWG attribution text")
	}
	if !strings.Contains(html, `href="https://rawg.io"`) {
		t.Error("footer is missing the link to rawg.io")
	}
}

func TestLayout_RendersShellAroundContent(t *testing.T) {
	html := renderHome(t)

	for _, fragment := range []string{
		"<!doctype html>",
		`<html lang="pt-BR">`,
		"<header",
		"<main",
		"<footer",
		`<link rel="stylesheet" href="/static/css/output.css">`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("layout output is missing %q", fragment)
		}
	}
}
