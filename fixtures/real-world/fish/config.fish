# Source: synthetic — representative fish shell config based on common
# patterns from https://github.com/oh-my-fish/oh-my-fish and the fish
# shell docs. License: MIT-style.
#
# config.fish — interactive shell configuration

set -gx EDITOR nvim
set -gx PAGER less
set -gx PATH $HOME/.local/bin $PATH

# Source helper modules — exercises IMPORTS edges (#371).
source $HOME/.config/fish/conf.d/aliases.fish
. $HOME/.config/fish/conf.d/path.fish

# ---------------------------------------------------------------------------
# Prompt
# ---------------------------------------------------------------------------
function fish_prompt --description 'Write out the prompt'
    set -l last_status $status
    set -l cwd (prompt_pwd)
    if test $last_status -ne 0
        set_color red
    else
        set_color green
    end
    echo -n "$cwd \$ "
    set_color normal
end

function fish_right_prompt
    set -l git_branch (git rev-parse --abbrev-ref HEAD 2>/dev/null)
    if test -n "$git_branch"
        echo -n "($git_branch)"
    end
end

# ---------------------------------------------------------------------------
# Aliases as functions (fish convention — abbr is preferred for aliases but
# functions are extractable and idiomatic for multi-line wrappers).
# ---------------------------------------------------------------------------
function ll --description 'long listing'
    ls -lAh $argv
end

function gs --description 'git status shortcut'
    git status --short --branch $argv
end

function mkcd --description 'mkdir -p && cd'
    mkdir -p $argv[1]
    and cd $argv[1]
end

# ---------------------------------------------------------------------------
# Greeting — fish calls this at interactive shell start.
# ---------------------------------------------------------------------------
function fish_greeting
    echo "Welcome to fish — the friendly interactive shell."
end

# ---------------------------------------------------------------------------
# Completions — declared inline for convenience. Real installs put these
# under ~/.config/fish/completions/ but inline forms appear in dotfiles
# repos and tooling setup scripts.
# ---------------------------------------------------------------------------
complete --command kubectl --no-files --arguments '(kubectl completion fish | source)'
complete -c docker -n '__fish_seen_subcommand_from run' -l rm -d 'Remove container after exit'
complete --command git --condition '__fish_git_using_command switch' --arguments '(__fish_git_branches)'
