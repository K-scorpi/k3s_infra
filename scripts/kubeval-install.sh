curl -L "https://github.com/instrumenta/kubeval/releases/latest/download/kubeval-darwin-amd64.tar.gz" | tar xz

# Сделайте исполняемым и переместите в PATH
chmod +x kubeval
sudo mv kubeval /usr/local/bin/