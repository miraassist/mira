# Waiting videos

`wait-de.mp4` and `wait-en.mp4` are made by Mira's maintainers with ffmpeg (a ring drawn with `geq`, the words with
`drawtext` in DejaVu Sans), 960×480, 60 s, without sound (docs/spec/waiting-indicator.md). The recipe:

    A='if(lt(atan2(X-480,215-Y),0),atan2(X-480,215-Y)+2*PI,atan2(X-480,215-Y))'; R='hypot(X-480,Y-215)'
    ON="gt($A,2*PI*T/60)"; RING="between($R,130,160)"
    ffmpeg -f lavfi -i "color=c=0x0f0f14:s=960x480:r=15:d=60,format=rgb24" \
      -vf "geq=r='if($RING,if($ON,64,45),15)':g='if($RING,if($ON,224,45),15)':b='if($RING,if($ON,208,50),20)',drawtext=fontfile=DejaVuSans.ttf:text='Ich warte auf dein Gerät …':fontcolor=white:fontsize=34:x=(w-tw)/2:y=h-62,format=yuv420p" \
      -an -c:v libx264 -preset veryfast -crf 28 -movflags +faststart wait-de.mp4
