/** base64 без data:-приставки. Кусками: `String.fromCharCode(...всё)`
 *  на мегабайтах переполняет стек вызова. */
export function toBase64(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf)
  let out = ''
  for (let i = 0; i < bytes.length; i += 0x8000) {
    out += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return btoa(out)
}
