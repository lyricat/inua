package main
import ("fmt";"os";"github.com/lyricat/inua/internal/desktop";"github.com/lyricat/inua/internal/shell")
func main(){
  es := desktop.Scan(desktop.DataDirs(), os.Getenv("LANG"))
  fmt.Println("entries:", len(es))
  a := shell.NewAppIndex(es)
  for _, c := range []string{"org.kde.dolphin","Code","firefox"} {
    if e := a.ForClass(c); e != nil { fmt.Printf("%s -> %q icon %q\n", c, shell.AppName(e), e.Icon) } else { fmt.Println(c, "-> nil") }
  }
}
