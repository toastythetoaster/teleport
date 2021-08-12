export type TdaMessage = ArrayBuffer;

// 0 is left button, 1 is middle button, 2 is right button
export type MouseButton = 0 | 1 | 2;

export enum ButtonState {
  UP = 0,
  DOWN = 1,
}

// TdaCodec provides an api for encoding and decoding teleport desktop access protocol messages [1]
// Buffers in TdaCodec are manipulated as DataView's [2] in order to give us low level control
// of endianness (defaults to big endian, which is what we want), as opposed to using *Array
// objects [3] which use the platform's endianness.
// [1] https://github.com/gravitational/teleport/blob/master/rfd/0037-desktop-access-protocol.md
// [2] https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/DataView
// [3] https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Int32Array
export default class TdaCodec {
  // TODO: make these cross-browser, some key codes depend on the browser:
  // https://developer.mozilla.org/en-US/docs/Web/API/KeyboardEvent/code/code_values

  private _keyScancodes = {
    Unidentified: 0x0000,
    "": 0x0000,
    Escape: 0x0001,
    Digit0: 0x0002,
    Digit1: 0x0003,
    Digit2: 0x0004,
    Digit3: 0x0005,
    Digit4: 0x0006,
    Digit5: 0x0007,
    Digit6: 0x0008,
    Digit7: 0x0009,
    Digit8: 0x000a,
    Digit9: 0x000b,
    Minus: 0x000c,
    Equal: 0x000d,
    Backspace: 0x000e,
    Tab: 0x000f,
    KeyQ: 0x0010,
    KeyW: 0x0011,
    KeyE: 0x0012,
    KeyR: 0x0013,
    KeyT: 0x0014,
    KeyY: 0x0015,
    KeyU: 0x0016,
    KeyI: 0x0017,
    KeyO: 0x0018,
    KeyP: 0x0019,
    BracketLeft: 0x001a,
    BracketRight: 0x001b,
    Enter: 0x001c,
    ControlLeft: 0x001d,
    KeyA: 0x001e,
    KeyS: 0x001f,
    KeyD: 0x0020,
    KeyF: 0x0021,
    KeyG: 0x0022,
    KeyH: 0x0023,
    KeyJ: 0x0024,
    KeyK: 0x0025,
    KeyL: 0x0026,
    Semicolon: 0x0027,
    Quote: 0x0028,
    Backquote: 0x0029,
    ShiftLeft: 0x002a,
    Backslash: 0x002b,
    KeyZ: 0x002c,
    KeyX: 0x002d,
    KeyC: 0x002e,
    KeyV: 0x002f,
    KeyB: 0x0030,
    KeyN: 0x0031,
    KeyM: 0x0032,
    Comma: 0x0033,
    Period: 0x0034,
    Slash: 0x0035,
    ShiftRight: 0x0036,
    NumpadMultiply: 0x0037,
    AltLeft: 0x0038,
    Space: 0x0039,
    CapsLock: 0x003a,
    F1: 0x003b,
    F2: 0x003c,
    F3: 0x003d,
    F4: 0x003e,
    F5: 0x003f,
    F6: 0x0040,
    F7: 0x0041,
    F8: 0x0042,
    F9: 0x0043,
    F10: 0x0044,
    Pause: 0x0045,
    ScrollLock: 0x0046,
    Numpad7: 0x0047,
    Numpad8: 0x0048,
    Numpad9: 0x0049,
    NumpadSubtract: 0x004a,
    Numpad4: 0x004b,
    Numpad5: 0x004c,
    Numpad6: 0x004d,
    NumpadAdd: 0x004e,
    Numpad1: 0x004f,
    Numpad2: 0x0050,
    Numpad3: 0x0051,
    Numpad0: 0x0052,
    NumpadDecimal: 0x0053,
    PrintScreen: 0x0054,
    IntlBackslash: 0x0056,
    F11: 0x0057,
    F12: 0x0058,
    NumpadEqual: 0x0059,
    F13: 0x0064,
    F14: 0x0065,
    F15: 0x0066,
    F16: 0x0067,
    F17: 0x0068,
    F18: 0x0069,
    F19: 0x006a,
    F20: 0x006b,
    F21: 0x006c,
    F22: 0x006d,
    F23: 0x006e,
    KanaMode: 0x0070,
    Lang2: 0x0071,
    Lang1: 0x0072,
    IntlRo: 0x0073,
    F24: 0x0076,
    Convert: 0x0079,
    NonConvert: 0x007b,
    IntlYen: 0x007d,
    NumpadComma: 0x007e,
    MediaTrackPrevious: 0xe010,
    MediaTrackNext: 0xe019,
    NumpadEnter: 0xe01c,
    ControlRight: 0xe01d,
    AudioVolumeMute: 0xe020,
    LaunchApp2: 0xe021,
    MediaPlayPause: 0xe022,
    MediaStop: 0xe024,
    AudioVolumeDown: 0xe02e,
    AudioVolumeUp: 0xe030,
    BrowserHome: 0xe032,
    NumpadDivide: 0xe035,
    AltRight: 0xe038,
    NumLock: 0xe045,
    Home: 0xe047,
    ArrowUp: 0xe048,
    PageUp: 0xe049,
    ArrowLeft: 0xe04b,
    ArrowRight: 0xe04d,
    End: 0xe04f,
    ArrowDown: 0xe050,
    PageDown: 0xe051,
    Insert: 0xe052,
    Delete: 0xe053,
    MetaLeft: 0xe05b,
    MetaRight: 0xe05c,
    ContextMenu: 0xe05d,
    Power: 0xe05e,
    BrowserSearch: 0xe065,
    BrowserFavorites: 0xe066,
    BrowserRefresh: 0xe067,
    BrowserStop: 0xe068,
    BrowserForward: 0xe069,
    BrowserBack: 0xe06a,
    LaunchApp1: 0xe06b,
    LaunchMail: 0xe06c,
    LaunchMediaPlayer: 0xe06d,
  };

  // encMouseMove encodes a mouse move event
  encMouseMove(x: number, y: number): TdaMessage {
    const buffer = new ArrayBuffer(9);
    const view = new DataView(buffer);
    view.setUint8(0, 3);
    view.setUint32(1, x);
    view.setUint32(5, y);
    return buffer;
  }

  // encMouseButton encodes a mouse button action
  encMouseButton(button: MouseButton, state: ButtonState): TdaMessage {
    const buffer = new ArrayBuffer(3);
    const view = new DataView(buffer);
    view.setUint8(0, 4);
    view.setUint8(1, button);
    view.setUint8(2, state);
    return buffer;
  }

  // encKeyboard encodes a keyboard action.
  // Returns null if an unsupported code is passed
  encKeyboard(code: string, state: ButtonState): TdaMessage {
    const scanCode = this._keyScancodes[code];
    if (!scanCode) {
      return null;
    }
    const buffer = new ArrayBuffer(6);
    const view = new DataView(buffer);
    view.setUint8(0, 5);
    view.setUint32(1, scanCode);
    view.setUint8(5, state);
    return buffer;
  }
}
