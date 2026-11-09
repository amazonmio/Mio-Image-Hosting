// Keep setup and password changes aligned with passwordValid on the server.
export function validPassword(password: string): boolean {
  const characters = [...password]
  if (!password.trim() || characters.length < 10) {
    return false
  }
  if (new TextEncoder().encode(password).length > 256) {
    return false
  }
  // Iteration combines valid surrogate pairs; remaining surrogates are malformed.
  return characters.every((character) => {
    const code = character.codePointAt(0)!
    return code < 0xd800 || code > 0xdfff
  })
}

export function validImageName(name: string) {
  const value = name.trim()
  const chars = [...value]
  if (chars.length < 1 || chars.length > 180) {
    return false
  }
  if (/[/\\]/.test(value)) {
    return false
  }
  return chars.every((char) => {
    const code = char.codePointAt(0) ?? 0
    // Match Go unicode.IsControl and its rejection of U+FFFD. Lone UTF-16
    // surrogates become U+FFFD when decoded by the Go JSON parser.
    return (
      code >= 0x20 &&
      !(code >= 0x7f && code <= 0x9f) &&
      code !== 0xfffd &&
      !(code >= 0xd800 && code <= 0xdfff)
    )
  })
}
