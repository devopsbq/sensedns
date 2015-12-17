docker network create -d overlay prueba
docker network create -d overlay test
docker network create -d overlay debug
RANDOM=$$$(date +%s)
IDS=()
IMAGES=("mongo" "redis" "nginx" "debian")
NETWORKS=("prueba" "test" "debug")
while [ 1 ]; do
    if [ $RANDOM -gt "20000" ] && [ ${#IDS[@]} -gt "0" ]; then
        ID=${IDS[$RANDOM % ${#IDS[@]}]}
        echo "docker rm -f $ID"
        docker rm -f $ID > /dev/null 2>&1
        IDS=("${IDS[@]/$ID}")
    else
        NET=${NETWORKS[$RANDOM % ${#NETWORKS[@]}]}
        IMAGE=${IMAGES[$RANDOM % ${#IMAGES[@]}]}
        HOSTNAME=$IMAGE
        echo "docker run -d --net $NET --hostname $HOSTNAME $IMAGE"
        ID=$(docker run -d --net $NET --hostname $HOSTNAME $IMAGE)
        echo "$ID"
        IDS+=($ID)
    fi
    sleep $[ ($RANDOM % 7)  + 1 ]s
done
