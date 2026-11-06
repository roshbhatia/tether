package command

import (
	"reflect"
	"testing"

	"github.com/roshbhatia/go-utils/completion"
	"github.com/roshbhatia/tether/internal/rank"
)

func TestCompletionVocabularyMatchesRanking(t *testing.T) {
	t.Parallel()
	commands := append([]completion.Command{TSHSpecification()}, Specification().Subcommands...)
	for _, command := range commands {
		for _, flag := range command.Flags {
			var expected []string
			switch flag.Name {
			case "mode":
				expected = rank.ModeNames()
			case "pin":
				expected = rank.TierNames()
			default:
				continue
			}
			if !reflect.DeepEqual(flag.Values, expected) {
				t.Fatalf("%s --%s: %v", command.Name, flag.Name, flag.Values)
			}
		}
	}
}
