package components

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func renderStars(t *testing.T, selected int16) string {
	t.Helper()

	var sb strings.Builder
	if err := StarRatingInput(selected).Render(context.Background(), &sb); err != nil {
		t.Fatalf("rendering star rating input: %v", err)
	}

	return sb.String()
}

func TestStarRatingInput_RendersTenNativeRadios(t *testing.T) {
	html := renderStars(t, 0)

	if got := strings.Count(html, `type="radio"`); got != 10 {
		t.Errorf("radio count = %d, want 10 (half-star steps from 0.5 to 5)", got)
	}
	if got := strings.Count(html, `name="rating"`); got != 10 {
		t.Errorf("inputs named rating = %d, want 10", got)
	}

	for rating := 1; rating <= 10; rating++ {
		if !strings.Contains(html, fmt.Sprintf(`value="%d"`, rating)) {
			t.Errorf("missing radio for rating %d", rating)
		}
		if !strings.Contains(html, fmt.Sprintf(`for="rating-%d"`, rating)) {
			t.Errorf("rating %d has no associated label", rating)
		}
	}
}

func TestStarRatingInput_BlankFormHasNoSelection(t *testing.T) {
	if html := renderStars(t, 0); strings.Contains(html, "checked") {
		t.Error("a blank form should render with no rating pre-selected")
	}
}

func TestStarRatingInput_PreselectsGivenRating(t *testing.T) {
	html := renderStars(t, 7)

	if got := strings.Count(html, "checked"); got != 1 {
		t.Fatalf("checked attributes = %d, want exactly 1", got)
	}

	// The checked attribute must sit on the radio carrying value="7".
	idx := strings.Index(html, `value="7"`)
	if idx < 0 {
		t.Fatal(`missing the radio with value="7"`)
	}
	if end := strings.Index(html[idx:], ">"); !strings.Contains(html[idx:idx+end], "checked") {
		t.Errorf("checked is not on the rating 7 radio: %q", html[idx:idx+end])
	}
}

func TestStarRatingInput_LabelsAreAccessible(t *testing.T) {
	html := renderStars(t, 0)

	for _, label := range []string{"meia estrela", "1 estrela", "2 estrelas e meia", "5 estrelas"} {
		if !strings.Contains(html, label) {
			t.Errorf("missing accessible label %q", label)
		}
	}
}
