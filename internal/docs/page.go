package docs

import (
	"fmt"
	"html"
	"strings"
)

// Оболочка страницы: стиль внутри файла, ни одной внешней ссылки.
//
// Так же, как чарт не тянет зависимостей, а образ не ходит в интернет:
// документацию читают там, где её открыли, — в закрытом контуре,
// с флешки, из папки на диске. Страница, просящая шрифт или стиль
// с чужого адреса, там показывает голый текст, а в худшем случае
// сообщает наружу, что её открыли.
//
// Цвета — те же, что у продукта, и обе темы: читают документацию тем же
// вечером и с тем же экраном.
const стиль = `
:root{--paper:#F4F6F4;--surface:#FFFFFF;--surface-2:#EAEEEB;--ink:#141D1B;
--ink-2:#3D4B47;--ink-3:#6E7C77;--rule:#D3DAD6;--accent:#0E6F5C;
--accent-soft:#DCEBE5;--radius:6px}
@media (prefers-color-scheme:dark){:root:not([data-theme="light"]){
--paper:#0D1312;--surface:#161E1C;--surface-2:#1E2826;--ink:#E6EDEA;
--ink-2:#CCD5D1;--ink-3:#B3BDBA;--rule:#27322F;--accent:#6DCBB2;
--accent-soft:#122C25}}
*{box-sizing:border-box}
body{margin:0;background:var(--paper);color:var(--ink);
font:16px/1.6 ui-sans-serif,system-ui,"Segoe UI",Roboto,sans-serif}
.sheet{max-width:44rem;margin:0 auto;padding:2rem 1.25rem 4rem}
nav{display:flex;flex-wrap:wrap;gap:.75rem;padding:.75rem 0 1.5rem;
border-bottom:1px solid var(--rule);margin-bottom:2rem}
nav a{color:var(--ink-2);text-decoration:none;padding:.25rem .5rem;
border-radius:var(--radius)}
nav a:hover{background:var(--surface-2);color:var(--ink)}
nav a[aria-current="page"]{background:var(--accent-soft);color:var(--accent);
font-weight:600}
h1{font-size:1.75rem;line-height:1.25;margin:0 0 1rem}
h2{font-size:1.3rem;margin:2.5rem 0 .75rem;padding-top:.75rem;
border-top:1px solid var(--rule)}
h3{font-size:1.05rem;margin:1.75rem 0 .5rem}
p{margin:0 0 1rem}
ul,ol{margin:0 0 1rem;padding-left:1.5rem}
li{margin:.35rem 0}
strong{font-weight:650}
a{color:var(--accent)}
/* Боковой отступ у кода мал намеренно: с большим знак препинания
   после кода отрывается от него, и «(README.md, ...)» читается
   как «( README.md , ...)». Видно это только на собранной странице. */
code{font:0.9em/1.4 ui-monospace,SFMono-Regular,Menlo,monospace;
background:var(--surface-2);padding:.08em .2em;border-radius:4px}
pre{background:var(--surface);border:1px solid var(--rule);
border-radius:var(--radius);padding:.75rem 1rem;overflow-x:auto}
pre code{background:none;padding:0}
/* Таблица шире текста: колонка в 44rem удобна для чтения абзацами,
   а таблице требований в трёх столбцах в ней тесно — она переносит
   каждую ячейку по два слова. На узком экране всё возвращается
   к ширине текста и листается вбок. */
.tablewrap{overflow-x:auto;margin:0 0 1.25rem;
width:min(60rem,92vw);margin-left:calc((100% - min(60rem,92vw))/2)}
table{border-collapse:collapse;width:100%;font-size:.95rem}
th,td{text-align:left;vertical-align:top;padding:.45rem .6rem;
border-bottom:1px solid var(--rule)}
th{font-size:.8rem;text-transform:uppercase;letter-spacing:.04em;
color:var(--ink-3);white-space:nowrap}
/* Снимок вписывается в колонку: в исходнике он шириной с монитор,
   и без ограничения страница листается вбок — на узком экране это
   первое, что ломается. */
figure{margin:1.5rem 0;padding:0}
img{max-width:100%;height:auto;display:block;border:1px solid var(--rule);
border-radius:var(--radius)}
figcaption{margin-top:.4rem;color:var(--ink-3);font-size:.85rem}
footer{margin-top:3rem;padding-top:1rem;border-top:1px solid var(--rule);
color:var(--ink-3);font-size:.85rem}
/* Печать. Страницу печатают на бумагу и в PDF, и там другое всё:
   тёмная тема стоит краски, оглавление ведёт в никуда, а разрыв
   посреди таблицы делает вторую половину нечитаемой.

   Цвета задаются явно, а не наследуются из темы: у читающего может
   стоять тёмная, и «печать» тогда означает лист, залитый чёрным. */
@media print{
  :root{--paper:#fff;--surface:#fff;--surface-2:#f2f2f2;--ink:#111;
  --ink-2:#333;--ink-3:#555;--rule:#bbb;--accent:#0a5c4c;--accent-soft:#eef5f2}
  body{font-size:10pt;line-height:1.45;background:#fff;color:#111}
  .sheet{max-width:none;padding:0}
  /* На бумаге воздух дороже: лист кончается, а прокрутки нет. */
  h1{font-size:16pt;margin:0 0 .5rem}
  h2{font-size:12pt;margin:1rem 0 .4rem;padding-top:.4rem}
  h3{font-size:10.5pt;margin:.7rem 0 .3rem}
  p,ul,ol,.tablewrap{margin-bottom:.6rem}
  li{margin:.15rem 0}
  th,td{padding:.25rem .5rem}
  nav{display:none}
  h1{page-break-before:always;page-break-after:avoid}
  h1:first-of-type{page-break-before:avoid}
  h2,h3{page-break-after:avoid}
  table,figure,pre,li{page-break-inside:avoid}
  .tablewrap{width:auto;margin-left:0;overflow:visible}
  a{color:inherit;text-decoration:underline}
  /* Ссылка на бумаге бесполезна, если не видно, куда она ведёт;
     но только внешняя — внутренние ведут на соседний раздел того же
     файла, и их адрес читателю не нужен. */
  a[href^="http"]::after{content:" (" attr(href) ")";font-size:.85em;color:#555}
  figure{page-break-inside:avoid}
  img{border-color:#ccc}
  /* Подпись при печати скромнее: на экране она отделена воздухом,
     на бумаге тот же воздух отправляет её на второй лист. */
  footer{page-break-before:avoid;margin-top:1rem;padding-top:.4rem;font-size:8pt}
}
:where(a,button):focus-visible{outline:2px solid var(--accent);outline-offset:2px}
`

// Ссылка в оглавлении.
type Пункт struct {
	Файл string
	Имя  string
}

// Собрать — готовый файл: голова, оглавление, тело, подпись.
//
// Оглавление одно на все страницы и стоит вверху каждой: читать
// документацию подряд не будут, а прыгать между тремя текстами —
// будут.
func Собрать(с Страница, оглавление []Пункт, версия string) string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"ru\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", html.EscapeString(с.Заголовок))
	fmt.Fprintf(&b, "<style>%s</style>\n</head>\n<body>\n<main class=\"sheet\">\n", стиль)

	b.WriteString("<nav aria-label=\"Разделы документации\">\n")
	for _, п := range оглавление {
		текущая := ""
		if п.Файл == с.Файл {
			текущая = ` aria-current="page"`
		}
		fmt.Fprintf(&b, `<a href="%s"%s>%s</a>`+"\n", п.Файл, текущая, html.EscapeString(п.Имя))
	}
	b.WriteString("</nav>\n")

	b.WriteString(с.HTML)

	// Версия в подписи: документация к продукту без версии отвечает
	// на вопрос «а это про какую вашу?» молчанием.
	fmt.Fprintf(&b, "<footer>Доска, версия %s. Страница собрана из %s — "+
		"правьте исходник, а не её.</footer>\n",
		html.EscapeString(версия), html.EscapeString(strings.TrimSuffix(с.Файл, ".html")+".md"))
	b.WriteString("</main>\n</body>\n</html>\n")
	return b.String()
}

// СобратьВсё — все разделы одним файлом.
//
// Нужен для двух случаев, и оба живые: отдать документацию одним
// вложением и напечатать её — браузер печатает то, что открыто, а не
// восемь страниц по ссылкам. Оглавление здесь настоящее, со ссылками
// внутрь файла: печатное содержание без номеров страниц бесполезно,
// зато на экране по нему ходят.
func СобратьВсё(страницы []Страница, версия string) string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"ru\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	b.WriteString("<title>Доска — документация</title>\n")
	fmt.Fprintf(&b, "<style>%s</style>\n</head>\n<body>\n<main class=\"sheet\">\n", стиль)

	b.WriteString("<h1>Доска — документация</h1>\n<p>Все разделы одним файлом. ")
	fmt.Fprintf(&b, "Версия %s.</p>\n", html.EscapeString(версия))
	b.WriteString("<h2>Содержание</h2>\n<ul>\n")
	for _, с := range страницы {
		// Имя раздела, а не заголовок файла: у обзора и у руководства
		// по установке заголовок один и тот же — «Доска», — и в общем
		// содержании они становились неразличимы.
		имя := с.Имя
		if имя == "" {
			имя = с.Заголовок
		}
		fmt.Fprintf(&b, `<li><a href="#%s">%s</a></li>`+"\n", якорь(с.Файл), html.EscapeString(имя))
	}
	b.WriteString("</ul>\n")

	for _, с := range страницы {
		fmt.Fprintf(&b, `<section id="%s">`+"\n", якорь(с.Файл))
		b.WriteString(с.HTML)
		b.WriteString("</section>\n")
	}
	b.WriteString("</main>\n</body>\n</html>\n")
	return b.String()
}

// якорь — имя раздела внутри общего файла. Из имени файла, чтобы
// ссылки между разделами оставались предсказуемыми.
func якорь(файл string) string {
	return strings.TrimSuffix(файл, ".html")
}
