import { crc32 } from 'node:zlib'

/**
 * Книга Excel для сквозных сценариев — собранная здесь же, без
 * библиотеки: zip без сжатия и три XML. Двоичный образец в репозитории
 * глазами не прочитать, а по этому видно, что в книге лежит.
 *
 * Ячейки — строки прямо в листе (`inlineStr`), числа — как есть:
 * так сохраняют и настоящие выгрузки, общие строки не обязательны.
 */
export function workbook(sheets: { name: string; rows: (string | number)[][] }[]): Buffer {
  const files: [string, string][] = [
    [
      'xl/workbook.xml',
      '<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>' +
        sheets.map((s, i) => `<sheet name="${esc(s.name)}" sheetId="${i + 1}" r:id="rId${i + 1}"/>`).join('') +
        '</sheets></workbook>',
    ],
    [
      'xl/_rels/workbook.xml.rels',
      '<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">' +
        sheets
          .map(
            (_, i) =>
              `<Relationship Id="rId${i + 1}" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet${i + 1}.xml"/>`,
          )
          .join('') +
        '</Relationships>',
    ],
    ...sheets.map((s, i): [string, string] => [
      `xl/worksheets/sheet${i + 1}.xml`,
      '<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>' +
        s.rows
          .map(
            (row, r) =>
              `<row r="${r + 1}">` +
              row
                .map((v, c) => {
                  const ref = String.fromCharCode(65 + c) + (r + 1)
                  return typeof v === 'number'
                    ? `<c r="${ref}"><v>${v}</v></c>`
                    : `<c r="${ref}" t="inlineStr"><is><t>${esc(v)}</t></is></c>`
                })
                .join('') +
              '</row>',
          )
          .join('') +
        '</sheetData></worksheet>',
    ]),
  ]
  return zip(files.map(([name, body]) => [name, Buffer.from(body)]))
}

function esc(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;')
}

/** Zip без сжатия: заголовок каждого файла, данные, оглавление, конец. */
function zip(files: [string, Buffer][]): Buffer {
  const parts: Buffer[] = []
  const central: Buffer[] = []
  let offset = 0
  for (const [name, data] of files) {
    const n = Buffer.from(name)
    const crc = crc32(data)
    const local = Buffer.alloc(30)
    local.writeUInt32LE(0x04034b50, 0)
    local.writeUInt16LE(20, 4)
    local.writeUInt32LE(crc, 14)
    local.writeUInt32LE(data.length, 18)
    local.writeUInt32LE(data.length, 22)
    local.writeUInt16LE(n.length, 26)
    parts.push(local, n, data)

    const entry = Buffer.alloc(46)
    entry.writeUInt32LE(0x02014b50, 0)
    entry.writeUInt16LE(20, 4)
    entry.writeUInt16LE(20, 6)
    entry.writeUInt32LE(crc, 16)
    entry.writeUInt32LE(data.length, 20)
    entry.writeUInt32LE(data.length, 24)
    entry.writeUInt16LE(n.length, 28)
    entry.writeUInt32LE(offset, 42)
    central.push(entry, n)
    offset += local.length + n.length + data.length
  }
  const dir = Buffer.concat(central)
  const end = Buffer.alloc(22)
  end.writeUInt32LE(0x06054b50, 0)
  end.writeUInt16LE(files.length, 8)
  end.writeUInt16LE(files.length, 10)
  end.writeUInt32LE(dir.length, 12)
  end.writeUInt32LE(offset, 16)
  return Buffer.concat([...parts, dir, end])
}
