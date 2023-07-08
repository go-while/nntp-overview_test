#!/bin/bash -e
cd test_overview/
OK=0; BAD=0;
while IFS= read file; do
    echo -n "check $file ... "
    test ! -e "$file" && let BAD="BAD+1" && echo " ERROR not found…" && continue
    HEAD=$(head -1 "$file"|wc -c)
    DATA="$(tail -2 "$file" | head -1 | tr -d '\0' | head -1)"
    FTIME=$(echo "$DATA"|cut -d, -f1|cut -d= -f2)
    FLAST=$(echo "$DATA"|cut -d, -f2|cut -d= -f2)
    INDEX=$(echo "$DATA"|cut -d, -f3|cut -d= -f2)
    B_END=$(echo "$DATA"|cut -d, -f4|cut -d= -f2)
    F_END=$(echo "$DATA"|cut -d, -f5|cut -d= -f2)
    FSIZE=$(du -b "$file"|cut -f1)
    MLAST=$(tail -5 "$file"|head -1|cut -f1)
    let TLAST="MLAST+1"
    let R_END="F_END - B_END"
    let SPACE="B_END - INDEX"
    count=0
    #echo "FTIME=$FTIME FLAST=$FLAST F_END=$F_END";
    if [[ $F_END -eq $FSIZE && $FLAST -eq $TLAST ]]; then
        let count="count+1"
    fi

    if [[ $HEAD -eq 128 && $R_END -eq 128 ]]; then
        let count="count+1"
    fi

    if [[ $INDEX -lt $B_END && $SPACE -gt 0 ]]; then
        let count="count+1"
    fi

    if [ $count -eq 3 ]; then
        echo -n "OK "; let OK="OK+1"
    else
        echo -n "BAD "; let BAD="BAD+1"
    fi

    echo "SPACE=$SPACE MLAST=$MLAST INDEX=$INDEX B_END=$B_END F_END=$F_END FSIZE=$FSIZE HEAD=$HEAD R_END=$R_END "
done< <(find . -mindepth 1 -maxdepth 1 -name "*.overview")
echo "$0 OK=$OK BAD=$BAD"
test $BAD -eq 0 && exit 0
exit 1
