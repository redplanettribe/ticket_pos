// A document just big enough for `react-dom/client` to mount a component in
// node, so a test can run its layout effects, which `react-dom/server` never
// does. It has no layout engine: a test says how wide things are through
// `setLayout`, and the scroll offset is clamped the way a browser clamps it.
//
// Importing this module installs the document. Import it before anything that
// imports `react-dom`, which decides at load whether it can use the DOM.
// `node --test` runs each test file in its own process, so the globals do not
// reach other files.

/** How wide an element is, and how wide its content is, in pixels. */
export type FakeLayout = (element: FakeElement) => { clientWidth: number; scrollWidth: number };

let layout: FakeLayout = () => ({ clientWidth: 0, scrollWidth: 0 });

/** Says how wide every element is from now on. */
export function setLayout(next: FakeLayout): void {
  layout = next;
}

type FakeStyle = Record<string, string> & {
  setProperty(name: string, value: string): void;
  removeProperty(name: string): void;
};

export class FakeElement {
  readonly childNodes: FakeElement[] = [];
  parentNode: FakeElement | null = null;
  readonly attributes: Record<string, string> = {};
  readonly style: FakeStyle;
  nodeValue: string | null = null;
  #scrollLeft = 0;

  readonly nodeType: number;
  readonly nodeName: string;
  readonly ownerDocument: FakeElement | null;

  constructor(nodeType: number, nodeName: string, ownerDocument: FakeElement | null) {
    this.nodeType = nodeType;
    this.nodeName = nodeName;
    this.ownerDocument = ownerDocument;
    const style = {} as FakeStyle;
    Object.defineProperties(style, {
      setProperty: { value: (name: string, value: string) => (style[name] = value) },
      removeProperty: { value: (name: string) => delete style[name] },
    });
    this.style = style;
  }

  get tagName(): string {
    return this.nodeName;
  }
  get firstChild(): FakeElement | null {
    return this.childNodes[0] ?? null;
  }
  get lastChild(): FakeElement | null {
    return this.childNodes.at(-1) ?? null;
  }
  get nextSibling(): FakeElement | null {
    const siblings = this.parentNode?.childNodes ?? [];
    return siblings[siblings.indexOf(this) + 1] ?? null;
  }

  get clientWidth(): number {
    return layout(this).clientWidth;
  }
  get scrollWidth(): number {
    return layout(this).scrollWidth;
  }
  get scrollLeft(): number {
    return this.#scrollLeft;
  }
  set scrollLeft(value: number) {
    const { clientWidth, scrollWidth } = layout(this);
    this.#scrollLeft = Math.min(Math.max(0, value), Math.max(0, scrollWidth - clientWidth));
  }

  appendChild(child: FakeElement): FakeElement {
    child.parentNode?.removeChild(child);
    this.childNodes.push(child);
    child.parentNode = this;
    return child;
  }
  insertBefore(child: FakeElement, before: FakeElement | null): FakeElement {
    if (before === null) {
      return this.appendChild(child);
    }
    child.parentNode?.removeChild(child);
    this.childNodes.splice(this.childNodes.indexOf(before), 0, child);
    child.parentNode = this;
    return child;
  }
  removeChild(child: FakeElement): FakeElement {
    this.childNodes.splice(this.childNodes.indexOf(child), 1);
    child.parentNode = null;
    return child;
  }

  setAttribute(name: string, value: unknown): void {
    this.attributes[name] = String(value);
  }
  getAttribute(name: string): string | null {
    return this.attributes[name] ?? null;
  }
  hasAttribute(name: string): boolean {
    return name in this.attributes;
  }
  removeAttribute(name: string): void {
    delete this.attributes[name];
  }

  addEventListener(): void {}
  removeEventListener(): void {}

  set textContent(value: string) {
    this.childNodes.length = 0;
    if (value) {
      this.appendChild(fakeText(value, this.ownerDocument));
    }
  }
}

function fakeText(value: string, owner: FakeElement | null): FakeElement {
  const text = new FakeElement(3, "#text", owner);
  text.nodeValue = value;
  return text;
}

const document = new FakeElement(9, "#document", null);
const body = new FakeElement(1, "BODY", document);
Object.assign(document, {
  body,
  activeElement: body,
  defaultView: globalThis,
  documentElement: new FakeElement(1, "HTML", document).appendChild(body).parentNode,
  createElement: (tag: string) => new FakeElement(1, tag.toUpperCase(), document),
  createTextNode: (value: string) => fakeText(value, document),
});

Object.assign(globalThis, {
  document,
  window: globalThis,
  HTMLIFrameElement: class {},
  IS_REACT_ACT_ENVIRONMENT: true,
});

/** A fresh element in the document to mount a root in. */
export function container(): FakeElement {
  return body.appendChild(new FakeElement(1, "DIV", document));
}
