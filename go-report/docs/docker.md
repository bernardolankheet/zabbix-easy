# Docker

## Build manual da imagem

```bash
docker build -t seuusuario/zabbix-easy:latest .
```

## Envio para Docker Hub

```bash
docker login
# Substitua 'seuusuario' pelo seu usuário Docker Hub
docker tag zabbix-easy:latest seuusuario/zabbix-easy:latest
docker push seuusuario/zabbix-easy:latest
```
