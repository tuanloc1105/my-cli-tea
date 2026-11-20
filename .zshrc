
# set -x
export CURRENT_UID=$(id -u)
export CURRENT_GID=$(id -g)

export DEV_KIT_LOCATION="$HOME/dev-kit"

export GOROOT="$DEV_KIT_LOCATION/go"
export GOPATH="$HOME/go"
export JAVA_HOME="$DEV_KIT_LOCATION/jdk"
export MAVEN_HOME="$DEV_KIT_LOCATION/maven"
export MAVEND_HOME="$DEV_KIT_LOCATION/mavend"
export GRADLE_HOME="$DEV_KIT_LOCATION/gradle"
export GRAALVM_HOME="$DEV_KIT_LOCATION/graalvm"
export RUST_HOME="$DEV_KIT_LOCATION/rust"

export PATH="$JAVA_HOME/bin:\
$GOROOT/bin:$GOPATH/bin:\
$MAVEN_HOME/bin:\
$MAVEND_HOME/bin:\
$GRADLE_HOME/bin:\
$GRAALVM_HOME/bin:\
$HOME/k8s_tool:\
$DEV_KIT_LOCATION/node/bin:\
$DEV_KIT_LOCATION/bun:\
$DEV_KIT_LOCATION/protoc/bin:\
$HOME/.venv/bin:\
$DEV_KIT_LOCATION/protoc/bin:\
$DEV_KIT_LOCATION/tool:\
$DEV_KIT_LOCATION/tool/7z:\
$DEV_KIT_LOCATION/uv:\
$HOME/cargo/bin:\
$HOME/rustc/bin:\
$HOME/.local/bin:\
$PATH"

alias cls="clear && printf '\e[3J'"

alias g="git"

alias ggc="git gc --prune=now --aggressive"

alias gssb="git status -sb"

alias ga="git add"
alias gaa="git add -A"

alias grs="git reset"
alias grh="git reset HEAD~"
alias grsa="git reset ."

alias grt="git restore"
alias grta="git restore ."

alias gcm="git commit -m"

alias gpl="git pull"
alias gpr="git pull --recurse-submodules"
alias gsu="git submodule update --remote --merge"
alias gsup="git submodule update --remote --merge && git add . && git commit -m \"chore: update submodules to latest\" && git push"

alias gp="git push"
alias gpo="git push origin"
alias gpf="git push --force origin"

alias gf="git fetch"

alias gs="git stash"
alias gspm="git stash push -m"
alias gsl="git stash list"
alias gsa="git stash apply"
alias gsp="git stash pop"
alias gsd="git stash drop"
alias gss="git stash show -p"

alias gl="cls ; git log"
alias gls="cls ; git log --summary"
alias glo="cls ; git log --oneline"

gla() {
  git log --after="$1"
}

glb() {
  git log --before="$1"
}

glbw() {
  # git log --after="2024-01-01" --before="2024-03-31"
  git log --after="$1" --before="$2"
}

alias gd="git diff"

alias gds="git diff --shortstat"

alias gshow="git show"

alias gcp="git cherry-pick"

alias gc="git checkout"
alias gcb="git checkout -b"
# alias gco="git checkout -b test origin/test"
gcob() {
  git checkout -b $1 origin/$1
}

alias gdb="git branch -D"
alias gdbo="git push -d origin"

alias gb="git branch"
alias gba="git branch -a"

alias gremote="git remote -v"

alias kube="kubectl"
alias k="kubectl"
alias ka="kubectl apply -f"
alias kd="kubectl delete -f"
alias kea="kubectl get events --all-namespaces  --sort-by='.metadata.creationTimestamp' --watch"

kr() {
  kubectl rollout restart deployment $1 -n $2 --kubeconfig ~/.kube/config
}

alias h="helm"
alias hl="helm lint ."
alias hs="helm show chart ."

hu() {
  helm uninstall $1 -n $2
}
hug() {
  helm upgrade -i --force $1 -n $2 --create-namespace .
}

# alias l='ls -CF'
alias lh="ls -lhF"
alias lha="ls -lhaF"
# Sort by modification time (newest first)
alias lst='ls -lt --color=auto'

# Sort by modification time (oldest first)
alias lstr='ls -ltr --color=auto'
alias l='cls ; ls -lhtr --color=auto'
alias lr='cls ; ls -lhtr -R --color=auto'
alias l1='cls ; ls -1htr --color=auto'

# Sort by access time (recently accessed files first)
alias lsat='ls -lu --color=auto'

# Sort by status change time (metadata changed, e.g. chmod/chown)
alias lsct='ls -lc --color=auto'

# Sort by modification time with human-readable sizes
alias lsth='ls -lhtr --color=auto'

# Show hidden files too (include dotfiles)
alias lsa='ls -lhatr --color=auto'
alias la='cls ; ls -lhatr --color=auto'
alias lar='cls ; ls -lhatr -R --color=auto'
alias la1='cls ; ls -1hatr --color=auto'

# alias dush="du -sh"
dush() {
  cls
  du -sh "$1" 2> /dev/null
}
# alias dusha="du -sh -- * .[!.]* 2>/dev/null | sort -h"
alias dusha='cls ; du -sh $(ls -A) 2>/dev/null | sort -h'
alias sds="cls ; sudo su"
alias sdi="cls ; sudo -i"
alias ds="cls ; docker stats --no-stream"
alias dip="cls ; docker inspect"
alias dps="cls ; docker ps"
# alias dpsi="docker ps --format \"table {{.ID}}\t{{.Names}}\t{{.Status}}\""
alias dpsi="cls ; docker ps --format \"table {{.ID}}\t{{.Names}}\t{{.Status}}\t{{.Ports}}\""
alias di="cls ; docker images"
alias dia="cls ; docker images -a"
alias dii="cls ; docker images | awk '{print \$1 \":\" \$2}'"
alias drmi="docker rmi"
dsrm() {
  docker stop $@
  docker rm $@
}
alias dpull="docker pull"
alias dpull64="docker pull --platform linux/amd64"
alias dstop="docker stop"
alias dstart="docker start"
alias dbuild="docker buildx build -t"
alias dbuild64="docker buildx build --platform linux/amd64 -t"
alias dbuildp="docker buildx build --pull -t"
alias drestart="docker restart"
alias drm="docker rm"
alias dv="docker volume"
alias dn="docker network"
dvexport() {
    docker run --rm -v "$1":/volume -v "$(pwd)":/backup ubuntu tar cvf /backup/"$1".tar -C /volume .
}
dvimport() {
    # $1 là tên file .tar (ví dụ: backup.tar)
    # $2 là tên volume mới muốn tạo (ví dụ: my_new_volume)

    if [ -z "$1" ] || [ -z "$2" ]; then
        echo "❌ Sai cú pháp rồi bạn ơi!"
        echo "👉 Cách dùng: dockerimport <tên_file_tar> <tên_volume_mới>"
        return 1
    fi

    echo "🔄 Đang tạo volume mới: $2..."
    docker volume create "$2" > /dev/null

    echo "📦 Đang bung lụa dữ liệu từ $1 vào $2..."
    docker run --rm -v "$2":/volume -v "$(pwd)":/backup ubuntu tar xvf /backup/"$1" -C /volume

    echo "✅ Xong rồi sạch sẽ rồi bạn!"
}
alias dvls="docker volume ls"
alias dnls="docker network ls"
alias dvrm="docker volume rm"
alias dnrm="docker network rm"
alias dpsa="docker ps -a"
alias dl="cls ; docker logs --tail=100"
alias dlf="cls ; docker logs -f --tail=100"
alias dbp="docker builder prune -a -f"
alias dhis="docker history"
# alias dpsai="docker ps -a --format \"table {{.ID}}\t{{.Names}}\t{{.Status}}\""
alias dpsai="docker ps -a --format \"table {{.ID}}\t{{.Names}}\t{{.Status}}\t{{.Ports}}\""
alias df="df -h"
alias dfd="df -h . /"
alias dfa="df -ha"
alias dcbuild="docker compose build"
alias dcbuildnc="docker compose build --no-cache"
alias dckill="docker compose kill"
alias dcstop="docker compose stop"
alias dcstart="docker compose start"
alias dcrm="docker compose rm -f"
alias dcup="docker compose up -d"
alias dcupbuild="docker compose up -d --build"
alias dcupnd="docker compose up -d --no-deps"
alias dcupbuildnd="docker compose up -d --no-deps --build"
alias dcdown="docker compose down"
alias dcdownv="docker compose down -v"
alias dclog="cls ; docker compose logs -f --tail=1000"
alias dcpull="docker compose pull"
alias dcrestart="docker compose restart"
alias dcstart="docker compose start"
alias dcstop="docker compose stop"
alias dsdv="docker system df -v"
alias dspf="docker system prune -f"
alias dipf="docker image prune -f"
alias dspa="docker system prune -a --volumes"
alias dsdf="docker system df -v"

# dcstop() {
#     docker compose stop "$@"
# }

dsave() {
  docker save -o $1.tar "$2"
}

dload() {
  docker load --input $1.tar
}

dex() {
  docker exec -it "$@" /bin/bash || docker exec -it "$@" /bin/sh
}

dexu() {
  docker exec -it -u $2 $1 /bin/bash || docker exec -it -u $2 $1 /bin/sh
}

imgstart() {
    docker run  --name $1 -it --rm -v $2:/work --network=host --entrypoint /bin/bash $3 || \
      docker run  --name $1 -it --rm -v $2:/work --network=host --entrypoint /bin/sh $3
}

# alias zip-tarz="tar czvf"
# alias unzip-tarz="tar xzvf"

# alias zip-tar="tar cvf"
# alias unzip-tar="tar xvf"

# Nén và giải nén .tar.gz (Gzip)
alias tgz-zip="tar -cvzf"
alias tgz-unzip="tar -xvzf"

# Nén và giải nén .tar (Chỉ gộp file, không nén)
alias tar-zip="tar -cvf"
alias tar-unzip="tar -xvf"

# Bonus: Nén siêu mạnh với .tar.xz
alias txz-zip="tar -cvJf"
alias txz-unzip="tar -xvJf"

alias tarls="tar -tvf"

alias tar-example="tar -cvf package.tar"
alias tgz-example="tar -cvzf package.tar.gz"
alias txz-example="tar -cvJf package.tar.xz"

alias 7z="7zz"
alias 7zzip="7zz a"
alias 7zzipp="7zz a -p123" # set 123 as password
alias 7zlist="7zz l"
alias 7zextract="7zz x"

alias rarzip="rar a"
alias rarlist="unrar l"
alias rarextract="unrar x"

alias zipp="zip -r"
alias zipextract="unzip"
alias ziplist="unzip -l"

alias debif="dpkg --info"
alias debl="dpkg --list"
alias debrm="dpkg --remove"
alias debi="dpkg --install"
alias debc="dpkg --contents"
alias debp="dpkg --purge"
alias debs="dpkg --status"
alias debll="dpkg -L"

alias kl="kill"
alias k9="kill -9"
alias k15="kill -15" # SIGTERM
alias kall="killall"

alias lzd="lazydocker && cls"
alias ks="k9s && cls"
alias mvnbuild="mvn -Dmaven.wagon.http.ssl.insecure=true -Dmaven.wagon.http.ssl.allowall=true -Dmaven.wagon.http.ssl.ignore.validity.dates=true -Dmaven.resolver.transport=wagon dependency:resolve clean install -DskipTests=true -Dfile.encoding=UTF8 -f pom.xml"
alias t="top -E g"
alias topc="clear ; ps -eo user,pid,%cpu,rss,cmd --sort=-%cpu | head -20 | awk '{printf \"%-10s %-8d %-10.2f%% %-10.2f MB \", \$1,  \$2,  \$3,  \$4/1024} { for ( x=5 ; x<=NF ; x++ ) { printf(\"%s \",\$x) } print \"\" }'"
alias topr="clear ; ps -eo user,pid,%cpu,rss,cmd --sort=-rss | head -20 | awk '{printf \"%-10s %-8d %-10.2f%% %-10.2f MB \", \$1,  \$2,  \$3,  \$4/1024} { for ( x=5 ; x<=NF ; x++ ) { printf(\"%s \",\$x) } print \"\" }'"

systop() {
  echo "=== CPU Usage ==="
  top -b -n1 | grep "Cpu(s)" | awk '{print "CPU Load: " $2 + $4 "%"}'
  echo "=== Memory Usage ==="
  free -h
}

# Alias giữ nguyên logic của bạn, hoạt động hoàn hảo trên Mac
alias wst="while true; do clear; systop; sleep 0.5; done"

alias pa="ps -aux | grep -v 'grep' | grep"
alias cl="wc -l"

alias p="python"
alias pver="python --version"
alias j="java"
alias jver="java -XshowSettings:properties -version"
alias m="mvn"
alias mver="mvn -version"
alias gdver="gradle --version"
alias gver="go version"
alias gbuild="CGO_ENABLED=0 go build -o ./go_app"
alias gdownload="go mod download"
alias gget="go get ./..."
alias ginit="go mod init $(basename $(pwd))"
alias gtidy="go mod tidy"
alias ggetu="go get -u ./..."

alias rt="cd .. && pwd"
alias o="open"
alias oo="open ."
# alias or="open -R"
# alias oor="open -R ."
or() {
  explorer.exe /select,"$(wslpath -w "$(realpath "$1")")"
}
alias home="open ~"
alias cdhome="cd ~"
alias down="open ~/Downloads"
alias cddown="cd ~/Downloads"
alias doc="open ~/Documents"
alias cddoc="cd ~/Documents"
alias dev="open $DEV_KIT_LOCATION"
alias cddev="cd $DEV_KIT_LOCATION"
alias m2="open ~/.m2"
alias cdm2="cd ~/.m2"
alias up="uptime"
alias u="uname -a"
alias now="date +\"%d/%m/%Y %H:%M:%S\""
alias dtree="tree -d"

alias remove="rm -R"
alias removef="rm -Rf"
alias cop="cp -R"
alias copf="cp -Rf"
alias createf="touch"
alias created="mkdir -p"
alias move="mv -i"
alias movef="mv -f"
# alias lf="find . -name"
alias lp="sudo ss -tulnp"

createdk() {
  current_directory=$(pwd)
  mkdir -p $1
  cd $1
  touch .keep
  cd $current_directory
}

# lf() {
#   find . -name "$1" 2> /dev/null
# }

lf() {
    local search_name="$1"
    shift # Loại bỏ tham số đầu tiên (tên file) khỏi danh sách $@

    if [ $# -eq 0 ]; then
        # Trường hợp 1: Không có thư mục loại trừ
        find . -iname "$search_name" 2> /dev/null
    else
        # Trường hợp 2: Có 1 hoặc nhiều thư mục loại trừ
        local exclude_args=()
        for dir in "$@"; do
            # Thêm logic: Nếu đã có phần tử, thêm -o (OR) vào trước
            [[ ${#exclude_args[@]} -gt 0 ]] && exclude_args+=("-o")
            exclude_args+=("-path" "./$dir")
        done

        # Thực thi lệnh find với mảng các thư mục cần prune
        find . \( "${exclude_args[@]}" \) -prune -o -iname "$search_name" -print 2> /dev/null
    fi
}

excf() {
  local file

  while IFS= read -r file; do
    [[ -f "$file" ]] || continue
    "$@" "$file"
  done < <(ls -1Atr)
}

exc() {
  local file

  while IFS= read -r file; do
    "$@" "$file"
  done < <(ls -1Atr)
}

alias 000='chmod -R 000'
alias 400='chmod -R 400'
alias 600='chmod -R 600'
alias 700='chmod -R 700'
alias 644='chmod -R 644'
alias 444='chmod -R 444'
alias 555='chmod -R 555'
alias 664='chmod -R 664'
alias 666='chmod -R 666'
alias 711='chmod -R 711'
alias 755='chmod -R 755'
alias 775='chmod -R 775'
alias 777='chmod -R 777'

alias tn="(echo quit) | telnet"

alias rl="readlink -f"

alias clip='perl -pe "chomp if eof" | xclip -selection clipboard'

rlc() {
  printf "'%s'" "$(rl "$1")" | clip
}

clipf () {
  printf "%s" "$(cat "$1")" | clip
}

# b64e() {
#   base64 -i $1 -o $2
# }

# alias la='ls -Alh'                # show hidden files
# alias ls='ls -aFh --color=always' # add colors and file type extensions
# alias lx='ls -lXBh'               # sort by extension
# alias lk='ls -lSrh'               # sort by size
# alias lc='ls -ltcrh'              # sort by change time
# alias lu='ls -lturh'              # sort by access time
# alias lr='ls -lRh'                # recursive ls
# alias lt='ls -ltrh'               # sort by date
# alias lm='ls -alh |more'          # pipe through 'more'
# alias lw='ls -xAh'                # wide listing format
# alias ll='ls -Fls'                # long listing format
# alias labc='ls -lap'              # alphabetical sort
# alias lf="ls -l | egrep -v '^d'"  # files only
# alias ldir="ls -l | egrep '^d'"   # directories only
# alias lla='ls -Al'                # List and Hidden Files
# alias las='ls -A'                 # Hidden Files
# alias lls='ls -l'                 # List

alias nf="fastfetch"

alias dostounix="sed -i -e 's/\r$//' "

alias sysupdate="sudo apt update ; sudo apt full-upgrade -y ; sudo aptitude upgrade -y"

alias apti="sudo apt -y install"
alias aptia="sudo apt -y install ./*.deb"
alias aptu="sudo apt remove --purge --auto-remove -y"
alias aptf="sudo apt list --installed 2> /dev/null | grep"
# dpkg-query -Wf '${Installed-Size}\t${Package}\n' | awk '{printf "%.2f MB\t%s\n", $1/1024, $2}' | sort -n
alias aptl="dpkg-query -Wf '\${Installed-Size}\\t\${Package}\\n' | awk '{printf \"%.2f MB\\t%s\\n\", \$1/1024, \$2}' | sort -n"
alias apthl="sudo apt-mark showhold"
alias aptrepo="cd /etc/apt/sources.list.d"

alias cdkr="cd /usr/share/keyrings"

# lfc() {
#   grep -Rn "$1" .
#   # grep -Rna $1 .
#   # grep -Hn $1 .
# }

lfc() {
    local search_term="$1"
    shift # Loại bỏ từ khóa tìm kiếm khỏi danh sách tham số $@

    if [ $# -eq 0 ]; then
        # Trường hợp 1: Không có thư mục loại trừ
        grep -Rn "$search_term" . 2> /dev/null
    else
        # Trường hợp 2: Có 1 hoặc nhiều thư mục loại trừ
        local exclude_flags=()
        for dir in "$@"; do
            exclude_flags+=(--exclude-dir="$dir")
        done

        # Thực thi grep với các flag loại trừ
        grep -Rn "${exclude_flags[@]}" "$search_term" . 2> /dev/null
    fi
}

gfh() {
  git diff --name-only $1 $2
}

curldlo() {
  curl -L $1 -o $2
}

alias curli='curl -i'
alias curldl='curl -LO'
alias curlv='curl -v'

alias curlget='curl -fsSL'

own() {
  chown -R "$1":"$2" "$3"
}

sown() {
  chown -R $(whoami) "$1"
}

prip() {
  ifconfig | grep "inet "
}

plip() {
  curl https://ipinfo.io/json 2> /dev/null | jq
}

createsym() {
    local source_file
    local symlink_file

    # Nhập đường dẫn file nguồn
    # Sử dụng cách tiếp cận tương thích cả Bash và Zsh cho lệnh read
    printf "Nhập đường dẫn file nguồn: "
    read -r source_file

    # Nhập đường dẫn symbolic link
    printf "Nhập đường dẫn symbolic link: "
    read -r symlink_file

    # Kiểm tra xem file nguồn có tồn tại không
    if [ ! -e "$source_file" ]; then
        echo "Lỗi: File nguồn không tồn tại!"
        return 1
    fi

    # Kiểm tra xem đường dẫn đích đã nhập chưa
    if [ -z "$symlink_file" ]; then
        echo "Lỗi: Đường dẫn symbolic link không được để trống!"
        return 1
    fi

    # Tiến hành tạo symbolic link
    if ln -s "$source_file" "$symlink_file"; then
        echo "Tạo thành công!"
    else
        echo "Lỗi: Không thể tạo symbolic link. Vui lòng kiểm tra lại quyền hoặc đường dẫn."
        return 1
    fi
}

b64e() {
    if [ "$#" -ne 2 ]; then
        printf '%s\n' "Cách dùng: b64e <duong_dan_file_goc> <duong_dan_file_txt_output>" >&2
        printf '%s\n' "Ví dụ:    b64e hinh_anh.png base64_output.txt" >&2
        return 2
    fi

    local input_file output_file output_dir output_name temp_file
    input_file=$1
    output_file=$2

    if [ ! -e "$input_file" ]; then
        printf "Lỗi: Không tìm thấy file '%s'\n" "$input_file" >&2
        return 1
    fi

    if [ -d "$output_file" ]; then
        printf "Đã xảy ra lỗi khi ghi file output: %s\n" "$output_file" >&2
        return 1
    fi

    if [ "$input_file" = "$output_file" ] || {
        [ -e "$output_file" ] && [ "$input_file" -ef "$output_file" ]
    }; then
        printf '%s\n' "Lỗi: File đầu vào và đầu ra phải khác nhau." >&2
        return 1
    fi

    case "$output_file" in
        */*)
            output_dir=${output_file%/*}
            [ -n "$output_dir" ] || output_dir=/
            ;;
        *)
            output_dir=.
            ;;
    esac
    output_name=${output_file##*/}

    temp_file=$(command mktemp "${output_dir}/.${output_name}.tmp.XXXXXX") || {
        printf "Đã xảy ra lỗi khi tạo file output: %s\n" "$output_file" >&2
        return 1
    }

    if ! (
        set -o pipefail
        command base64 < "$input_file" | command tr -d '\r\n' > "$temp_file"
    ); then
        command rm -f -- "$temp_file"
        printf '%s\n' "Đã xảy ra lỗi khi chuyển file sang Base64." >&2
        return 1
    fi

    if ! command mv -f -- "$temp_file" "$output_file"; then
        command rm -f -- "$temp_file"
        printf "Đã xảy ra lỗi khi ghi file output: %s\n" "$output_file" >&2
        return 1
    fi

    if ! command rm -f -- "$input_file"; then
        printf "Cannot remove %s\n" "$input_file" >&2
    fi

    printf "Thành công! Chuỗi Base64 đã được lưu tại: %s\n" "$output_file"
}

b64d() {
    if [ "$#" -ne 2 ]; then
        printf '%s\n' "Cách dùng: b64d <duong_dan_file_txt_base64> <duong_dan_file_khoi_phuc>" >&2
        printf '%s\n' "Ví dụ:    b64d base64_output.txt ket_qua_khoi_phuc.png" >&2
        return 2
    fi

    local input_file output_file output_dir output_name temp_file
    input_file=$1
    output_file=$2

    if [ ! -e "$input_file" ]; then
        printf "Lỗi: Không tìm thấy file text '%s'\n" "$input_file" >&2
        return 1
    fi

    if [ -d "$output_file" ]; then
        printf "Đã xảy ra lỗi khi ghi file output: %s\n" "$output_file" >&2
        return 1
    fi

    if [ "$input_file" = "$output_file" ] || {
        [ -e "$output_file" ] && [ "$input_file" -ef "$output_file" ]
    }; then
        printf '%s\n' "Lỗi: File đầu vào và đầu ra phải khác nhau." >&2
        return 1
    fi

    case "$output_file" in
        */*)
            output_dir=${output_file%/*}
            [ -n "$output_dir" ] || output_dir=/
            ;;
        *)
            output_dir=.
            ;;
    esac
    output_name=${output_file##*/}

    temp_file=$(command mktemp "${output_dir}/.${output_name}.tmp.XXXXXX") || {
        printf "Đã xảy ra lỗi khi tạo file output: %s\n" "$output_file" >&2
        return 1
    }

    if ! command base64 -d < "$input_file" > "$temp_file"; then
        command rm -f -- "$temp_file"
        printf '%s\n' "Đã xảy ra lỗi khi chuyển đổi ngược từ Base64." >&2
        return 1
    fi

    if ! command mv -f -- "$temp_file" "$output_file"; then
        command rm -f -- "$temp_file"
        printf "Đã xảy ra lỗi khi ghi file output: %s\n" "$output_file" >&2
        return 1
    fi

    if ! command rm -f -- "$input_file"; then
        printf "Cannot remove %s\n" "$input_file" >&2
    fi

    printf "Thành công! File gốc đã được khôi phục tại: %s\n" "$output_file"
}

rbg() {
  "$@" > /dev/null 2>&1 &
}

export TZ="Asia/Ho_Chi_Minh"

export TOP_SIZE_UNITS=GB

source ~/another-aliases 2> /dev/null || touch ~/another-aliases

alias dock-faster="defaults write com.apple.dock autohide-delay -float 0; defaults write com.apple.dock autohide-time-modifier -int 0;killall Dock"
alias dock-faster-undo="defaults write com.apple.dock autohide-delay -float 0.5; defaults write com.apple.dock autohide-time-modifier -int 0.5 ;killall Dock"

# eval "$(oh-my-posh init bash --config $HOME/git/my-experience/fast-oh-my-posh-theme.omp.jsonc)" 2> /dev/null

# oh-my-logo LCX 2> /dev/null

# export JAVA_VERSION=$(java -version 2>&1 | head -n 1 | awk '{print $3}')
# export MAVEN_VERSION=$(mvn -version 2>&1 | head -n 1 | awk '{print $3}')
# export GO_VERSION=$(go version 2>&1 | head -n 1 | awk '{print $3}')
# export NODE_VERSION=$(node -v)
# export PRIVATE_IP=$(hostname -I | awk '{print $1}')

# echo "Private IP: $(ipconfig getifaddr en0)"

sslcheck() {
  openssl s_client -connect $1:443
}

rmbracket() {
  local f dir base new
  for f in "$@"; do
    [ -e "$f" ] || { echo "Không tìm thấy: $f" >&2; continue; }
    dir=$(dirname -- "$f"); base=$(basename -- "$f")
    new=$(printf '%s' "$base" | sed -E 's/[][()]/ /g; s/ +/ /g; s/^ +//; s/ +$//; s/ +(\.[^. ]+)$/\1/')
    [ "$base" = "$new" ] && continue
    [ -e "$dir/$new" ] && { echo "Đã tồn tại: $dir/$new" >&2; continue; }
    mv -- "$f" "$dir/$new" && echo "$base -> $new"
  done
}
