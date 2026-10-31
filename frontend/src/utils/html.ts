import DOMPurify, { type Config } from 'dompurify'

const markdownHtmlPolicy: Config = {
  USE_PROFILES: { html: true },
  FORBID_TAGS: [
    'style',
    'script',
    'iframe',
    'frame',
    'frameset',
    'object',
    'embed',
    'form',
    'input',
    'button',
    'textarea',
    'select',
    'option',
    'link',
    'meta',
    'base',
  ],
  FORBID_ATTR: ['srcset', 'action', 'formaction', 'xlink:href'],
  ALLOW_DATA_ATTR: false,
}

export const sanitizeHtml = (html: string): string => DOMPurify.sanitize(html, markdownHtmlPolicy)
