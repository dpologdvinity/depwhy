package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/pflag"
)

// Shell completion: `depwhy completion bash|zsh|fish` prints a script for that
// shell. The scripts complete flags, flag values (directories, files, ecosystems)
// and package names. Package names come from the hidden --complete-packages flag,
// which loads the lockfiles that the words typed so far select (-C, -l, -e, -r)
// and prints every package name once. The flag lists inside the scripts are
// generated from the FlagSet, so a new flag is completed without editing them.

// completeFlag is the hidden flag the scripts call to list package names.
const completeFlag = "complete-packages"

// shells lists the shells `depwhy completion` supports.
var shells = []string{"bash", "zsh", "fish"}

// isCompletion reports whether the positional arguments are a completion request:
// exactly "completion" followed by one more word. A lone "completion" stays a
// query for a package of that name, and two package arguments are otherwise a
// usage error, so this form cannot hide a valid query.
func isCompletion(pos []string) bool { return len(pos) == 2 && pos[0] == "completion" }

// writeCompletion prints the script for shell, or returns a usage message for an
// unknown shell.
func writeCompletion(w io.Writer, fs *pflag.FlagSet, shell string) string {
	switch shell {
	case "bash":
		writeBash(w, fs)
	case "zsh":
		writeZsh(w, fs)
	case "fish":
		writeFish(w, fs)
	default:
		return "unknown shell " + shell + " (want " + strings.Join(shells, ", ") + ")"
	}
	return ""
}

// valueKind says how to complete a flag's value: "" for flags without one.
func valueKind(f *pflag.Flag) string {
	switch f.Name {
	case "dir", "venv":
		return "dir"
	case "lockfile":
		return "file"
	case "ecosystem":
		return "ecosystem"
	}
	// --heaviest takes its optional count only as --heaviest=N, so its next word
	// is not a value.
	if f.Value.Type() == "bool" || f.NoOptDefVal != "" {
		return ""
	}
	return "other"
}

// ecosystems are the values -e accepts, in the order the help text lists them.
const ecosystems = "npm python cargo go"

// visibleFlags returns the flags the scripts offer, sorted by name.
func visibleFlags(fs *pflag.FlagSet) []*pflag.Flag {
	var out []*pflag.Flag
	fs.VisitAll(func(f *pflag.Flag) {
		if !f.Hidden {
			out = append(out, f)
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// writeBash prints the bash script. It avoids mapfile and other bash 4 features,
// because macOS still ships bash 3.2.
func writeBash(w io.Writer, fs *pflag.FlagSet) {
	var words, dirs, files, eco, other []string
	for _, f := range visibleFlags(fs) {
		names := []string{"--" + f.Name}
		if f.Shorthand != "" {
			names = append(names, "-"+f.Shorthand)
		}
		words = append(words, names...)
		switch valueKind(f) {
		case "dir":
			dirs = append(dirs, names...)
		case "file":
			files = append(files, names...)
		case "ecosystem":
			eco = append(eco, names...)
		case "other":
			other = append(other, names...)
		}
	}
	// Each case arm is written only when some flag needs it: an empty pattern
	// would be a bash syntax error.
	var arms strings.Builder
	arm := func(names []string, body string) {
		if len(names) > 0 {
			fmt.Fprintf(&arms, "        %s) %s ;;\n", strings.Join(names, "|"), body)
		}
	}
	arm(dirs, `compopt -o filenames 2>/dev/null; _depwhy_lines < <(compgen -d -- "$cur"); return`)
	arm(files, `compopt -o filenames 2>/dev/null; _depwhy_lines < <(compgen -f -- "$cur"); return`)
	arm(eco, `_depwhy_lines < <(compgen -W "`+ecosystems+`" -- "$cur"); return`)
	arm(other, "return")
	fmt.Fprintf(w, `# bash completion for depwhy. Load it with:
#   source <(depwhy completion bash)

# _depwhy_lines appends each line of its input to COMPREPLY.
_depwhy_lines() {
    local line
    while IFS= read -r line; do COMPREPLY+=("$line"); done
}

_depwhy() {
    local cur=${COMP_WORDS[COMP_CWORD]} prev=${COMP_WORDS[COMP_CWORD-1]}
    COMPREPLY=()
    case $prev in
%s    esac
    if [[ $cur == -* ]]; then
        _depwhy_lines < <(compgen -W "%s" -- "$cur")
        return
    fi
    # Pass the words typed so far, so -C, -l, -e and -r select the same lockfiles.
    local args=() word
    for word in "${COMP_WORDS[@]:1:COMP_CWORD-1}"; do
        args+=("${word/#\~/$HOME}")
    done
    # Package names come from a lockfile, which may be hostile: never pass them to
    # compgen -W, which runs expansions (including command substitution) on its
    # list, and quote each one before it is inserted into the command line.
    local name quoted
    while IFS= read -r name; do
        if [[ $name == "$cur"* ]]; then
            printf -v quoted '%%q' "$name"
            COMPREPLY+=("$quoted")
        fi
    done < <(depwhy --%s "${args[@]}" 2>/dev/null)
}
complete -F _depwhy depwhy
`, arms.String(), strings.Join(words, " "), completeFlag)
}

// zshQuote escapes text for a description inside a zsh _arguments spec.
var zshQuote = strings.NewReplacer(`'`, `'\''`, `[`, `\[`, `]`, `\]`, `:`, `\:`)

// writeZsh prints the zsh script.
func writeZsh(w io.Writer, fs *pflag.FlagSet) {
	fmt.Fprint(w, `#compdef depwhy
# zsh completion for depwhy. Load it with:
#   source <(depwhy completion zsh)
# or save it as _depwhy in a directory on $fpath.

# _depwhy_packages completes package names from the lockfiles that the words
# typed so far select (-C, -l, -e, -r).
_depwhy_packages() {
    local -a pkgs
    pkgs=(${(f)"$(depwhy --`+completeFlag+` ${${(Q)words[2,CURRENT-1]}/#\~/$HOME} 2>/dev/null)"})
    compadd -a pkgs
}

_depwhy() {
    _arguments -s \
`)
	for _, f := range visibleFlags(fs) {
		action := ""
		switch valueKind(f) {
		case "dir":
			action = ":directory:_files -/"
		case "file":
			action = ":file:_files"
		case "ecosystem":
			action = ":ecosystem:(" + ecosystems + ")"
		case "other":
			action = ":" + f.Name + ": "
		}
		desc := zshQuote.Replace(f.Usage)
		// Repeatable flags may appear again; the others exclude themselves once used.
		repeat := f.Value.Type() == "stringArray"
		if f.Shorthand != "" {
			if repeat {
				fmt.Fprintf(w, "        '*'{-%s,--%s}'[%s]%s' \\\n", f.Shorthand, f.Name, desc, action)
			} else {
				fmt.Fprintf(w, "        '(-%s --%s)'{-%s,--%s}'[%s]%s' \\\n", f.Shorthand, f.Name, f.Shorthand, f.Name, desc, action)
			}
			continue
		}
		fmt.Fprintf(w, "        '--%s[%s]%s' \\\n", f.Name, desc, action)
	}
	fmt.Fprint(w, `        '*:package:_depwhy_packages'
}

if [[ $zsh_eval_context[-1] == loadautofunc ]]; then
    _depwhy "$@"
else
    compdef _depwhy depwhy
fi
`)
}

// fishQuote escapes text for a single-quoted fish string.
var fishQuote = strings.NewReplacer(`\`, `\\`, `'`, `\'`)

// writeFish prints the fish script.
func writeFish(w io.Writer, fs *pflag.FlagSet) {
	fmt.Fprint(w, `# fish completion for depwhy. Load it with:
#   depwhy completion fish | source
# or save it as ~/.config/fish/completions/depwhy.fish.

# Package names come from the lockfiles that the words typed so far select
# (-C, -l, -e, -r).
function __depwhy_packages
    depwhy --`+completeFlag+` (commandline -opc)[2..-1] 2>/dev/null
end

complete -c depwhy -f
complete -c depwhy -n 'not string match -q -- "-*" (commandline -ct)' -a '(__depwhy_packages)'
`)
	for _, f := range visibleFlags(fs) {
		line := "complete -c depwhy"
		if f.Shorthand != "" {
			line += " -s " + f.Shorthand
		}
		line += " -l " + f.Name
		switch valueKind(f) {
		case "dir":
			line += " -r -a '(__fish_complete_directories)'"
		case "file":
			line += " -r -F"
		case "ecosystem":
			line += " -x -a '" + ecosystems + "'"
		case "other":
			line += " -x"
		}
		fmt.Fprintf(w, "%s -d '%s'\n", line, fishQuote.Replace(f.Usage))
	}
}
