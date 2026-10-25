#include "inua_vt.h"

#include <ghostty/vt.h>

#include <stdbool.h>
#include <stdio.h>
#include <string.h>

/* A growing byte buffer, allocated through libghostty. */
typedef struct {
  uint8_t *p;
  size_t len, cap;
  bool failed;
} buf;

static void buf_put(buf *b, const uint8_t *data, size_t len) {
  if (b->failed) return;
  if (b->len + len > b->cap) {
    size_t cap = b->cap ? b->cap * 2 : 256;
    while (cap < b->len + len) cap *= 2;
    uint8_t *p = ghostty_alloc(NULL, cap);
    if (p == NULL) {
      b->failed = true;
      return;
    }
    if (b->len) memcpy(p, b->p, b->len);
    if (b->p) ghostty_free(NULL, b->p, b->cap);
    b->p = p;
    b->cap = cap;
  }
  memcpy(b->p + b->len, data, len);
  b->len += len;
}

static void buf_free(buf *b) {
  if (b->p) ghostty_free(NULL, b->p, b->cap);
}

static size_t utf8(uint32_t c, uint8_t *o);

/* A search match in viewport cells, start and end inclusive. */
typedef struct {
  uint16_t sx, ex;
  uint32_t sy, ey;
} span;

#define MAX_SPANS 512

struct inua_vt {
  GhosttyTerminal term;
  GhosttyRenderState render;
  GhosttyRenderStateRowIterator rows;
  GhosttyRenderStateRowCells cells;
  GhosttyKeyEncoder keys;
  GhosttyKeyEvent key;
  GhosttyMouseEncoder mouse;
  GhosttyMouseEvent mouse_ev;
  int32_t mouse_x, mouse_y; /* the cell of the last motion sent; -1 for none */
  GhosttySelectionGesture gesture;
  GhosttySelectionGestureEvent press, release, drag;
  /* What the terminal writes back to the program: answers to its
   * queries, or a paste's encoding. */
  buf out;
  /* The text a program last put on a clipboard; clip_at 0 for none, 1 the
   * clipboard, 2 the primary selection. */
  buf clip;
  int clip_at;
  GhosttySearch search;     /* NULL until a search */
  bool searching, search_changed;
  span spans[MAX_SPANS];    /* the matches in view, for this frame */
  size_t nspans;
  span current;
  bool has_current;
  buf prompts; /* inua_vt_prompts' records */
  buf notes;   /* inua_vt_notifications' records */
  /* Where the last input began, moved with the text: a shell redrawing
   * its prompt after a resize sends no marks again, and the prompt it
   * draws then would read as typed. */
  GhosttyTrackedGridRef input_at;
  bool primary; /* the last prompt drawn is a primary one, not a command's next line */
  bool answering;
  uint32_t cell_w, cell_h;
  bool dark; /* the background's scheme, once colours are set */
  bool colored;
};

int inua_vt_abi(void) { return INUA_VT_ABI; }

inua_vt *inua_vt_new(uint16_t cols, uint16_t rows) {
  uint8_t *p = ghostty_alloc(NULL, sizeof(inua_vt));
  if (p == NULL) return NULL;
  inua_vt *t = (inua_vt *)p;
  memset(t, 0, sizeof(*t));
  t->mouse_x = t->mouse_y = -1;
  if (ghostty_terminal_new(NULL, &t->term, cols, rows) != GHOSTTY_SUCCESS ||
      ghostty_render_state_new(NULL, &t->render) != GHOSTTY_SUCCESS ||
      ghostty_render_state_row_iterator_new(NULL, &t->rows) != GHOSTTY_SUCCESS ||
      ghostty_render_state_row_cells_new(NULL, &t->cells) != GHOSTTY_SUCCESS ||
      ghostty_key_encoder_new(NULL, &t->keys) != GHOSTTY_SUCCESS ||
      ghostty_key_event_new(NULL, &t->key) != GHOSTTY_SUCCESS ||
      ghostty_mouse_encoder_new(NULL, &t->mouse) != GHOSTTY_SUCCESS ||
      ghostty_mouse_event_new(NULL, &t->mouse_ev) != GHOSTTY_SUCCESS ||
      ghostty_selection_gesture_new(NULL, &t->gesture) != GHOSTTY_SUCCESS ||
      ghostty_selection_gesture_event_new(NULL, &t->press, GHOSTTY_SELECTION_GESTURE_EVENT_TYPE_PRESS) != GHOSTTY_SUCCESS ||
      ghostty_selection_gesture_event_new(NULL, &t->release, GHOSTTY_SELECTION_GESTURE_EVENT_TYPE_RELEASE) != GHOSTTY_SUCCESS ||
      ghostty_selection_gesture_event_new(NULL, &t->drag, GHOSTTY_SELECTION_GESTURE_EVENT_TYPE_DRAG) != GHOSTTY_SUCCESS) {
    inua_vt_free(t);
    return NULL;
  }
  return t;
}

void inua_vt_free(inua_vt *t) {
  if (t == NULL) return;
  buf_free(&t->out);
  buf_free(&t->clip);
  buf_free(&t->prompts);
  buf_free(&t->notes);
  ghostty_tracked_grid_ref_free(t->input_at);
  if (t->drag) ghostty_selection_gesture_event_free(t->drag);
  if (t->release) ghostty_selection_gesture_event_free(t->release);
  if (t->press) ghostty_selection_gesture_event_free(t->press);
  if (t->gesture) ghostty_selection_gesture_free(t->gesture, t->term);
  if (t->search) ghostty_search_free(t->search);
  if (t->mouse_ev) ghostty_mouse_event_free(t->mouse_ev);
  if (t->mouse) ghostty_mouse_encoder_free(t->mouse);
  ghostty_key_event_free(t->key);
  ghostty_key_encoder_free(t->keys);
  ghostty_render_state_row_cells_free(t->cells);
  ghostty_render_state_row_iterator_free(t->rows);
  ghostty_render_state_free(t->render);
  ghostty_terminal_free(t->term);
  ghostty_free(NULL, (uint8_t *)t, sizeof(inua_vt));
}

void inua_vt_write(inua_vt *t, const uint8_t *data, size_t len) {
  ghostty_terminal_vt_write(t->term, data, len);
}

void inua_vt_defaults(inua_vt *t, uint32_t scrollback, uint32_t cursor_style, uint32_t blink) {
  size_t lines = scrollback;
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_SCROLLBACK_MAX_LINES, &lines);
  GhosttyTerminalCursorStyle style = (GhosttyTerminalCursorStyle)cursor_style;
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_DEFAULT_CURSOR_STYLE, &style);
  bool b = blink != 0;
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_DEFAULT_CURSOR_BLINK, &b);
  /* The cursor now, as a program would have it after resetting it. */
  const uint8_t reset[] = "\x1b[0 q";
  ghostty_terminal_vt_write(t->term, reset, sizeof(reset) - 1);
}

/* hyperlink is the OSC 8 URI of a viewport cell into b; false for none. */
static bool hyperlink(inua_vt *t, uint16_t x, uint16_t y, buf *b) {
  GhosttyGridRef ref = GHOSTTY_INIT_SIZED(GhosttyGridRef);
  GhosttyPoint pt = {.tag = GHOSTTY_POINT_TAG_VIEWPORT, .value = {.coordinate = {.x = x, .y = y}}};
  if (ghostty_terminal_grid_ref(t->term, pt, &ref) != GHOSTTY_SUCCESS) return false;
  uint8_t tmp[2048];
  size_t n = 0;
  if (ghostty_grid_ref_hyperlink_uri(&ref, tmp, sizeof(tmp), &n) != GHOSTTY_SUCCESS || n == 0) return false;
  b->len = 0;
  buf_put(b, tmp, n);
  return !b->failed;
}

/* A URL's characters, as most terminals take them. */
static bool url_char(uint32_t c) {
  if (c <= ' ' || c == 0x7f) return false;
  return c >= 0x80 || strchr("<>\"`{}|\\^", (int)c) == NULL;
}

static bool scheme_at(const uint32_t *row, int n, int i, int *after) {
  static const char *schemes[] = {"https://", "http://", "file://", "ftp://", "ssh://", "mailto:", "git://"};
  for (size_t s = 0; s < sizeof(schemes) / sizeof(schemes[0]); s++) {
    const char *p = schemes[s];
    int k = 0;
    while (p[k] && i + k < n && row[i + k] == (uint32_t)p[k]) k++;
    if (!p[k]) {
      *after = i + k;
      return true;
    }
  }
  return false;
}

int inua_vt_link_at(inua_vt *t, uint16_t x, uint16_t y, uint8_t **out, size_t *len, uint16_t *x0, uint16_t *x1) {
  *out = NULL;
  *len = 0;
  uint16_t cols = 0;
  ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_COLS, &cols);
  if (x >= cols) return 0;
  buf uri = {0};
  int a = x, z = x;
  if (hyperlink(t, x, y, &uri)) {
    /* The cells around with the same link. */
    buf other = {0};
    while (a > 0 && hyperlink(t, (uint16_t)(a - 1), y, &other) && other.len == uri.len && memcmp(other.p, uri.p, uri.len) == 0) a--;
    while (z + 1 < cols && hyperlink(t, (uint16_t)(z + 1), y, &other) && other.len == uri.len && memcmp(other.p, uri.p, uri.len) == 0) z++;
    buf_free(&other);
  } else {
    /* The row's text a cell each (a wide character's tail is 0). */
    uint32_t row[1024];
    int n = cols < 1024 ? cols : 1024;
    for (int i = 0; i < n; i++) {
      row[i] = ' ';
      GhosttyGridRef ref = GHOSTTY_INIT_SIZED(GhosttyGridRef);
      GhosttyPoint pt = {.tag = GHOSTTY_POINT_TAG_VIEWPORT, .value = {.coordinate = {.x = (uint16_t)i, .y = y}}};
      if (ghostty_terminal_grid_ref(t->term, pt, &ref) != GHOSTTY_SUCCESS) continue;
      GhosttyCell cell = 0;
      GhosttyCellWide wide = GHOSTTY_CELL_WIDE_NARROW;
      if (ghostty_grid_ref_cell(&ref, &cell) == GHOSTTY_SUCCESS) ghostty_cell_get(cell, GHOSTTY_CELL_DATA_WIDE, &wide);
      if (wide == GHOSTTY_CELL_WIDE_SPACER_TAIL) { row[i] = 0; continue; }
      uint32_t cps[4];
      size_t k = 0;
      if (ghostty_grid_ref_graphemes(&ref, cps, 4, &k) == GHOSTTY_SUCCESS && k > 0) row[i] = cps[0];
    }
    if (x >= n || !url_char(row[x])) return 0;
    int s = x, e = x;
    while (s > 0 && (url_char(row[s - 1]) || row[s - 1] == 0)) s--;
    while (e + 1 < n && (url_char(row[e + 1]) || row[e + 1] == 0)) e++;
    /* The URL starts at a scheme within the word, before the cell. */
    int start = -1, after = 0;
    for (int i = s; i <= x && start < 0; i++)
      if (scheme_at(row, e + 1, i, &after)) start = i;
    if (start < 0 || after > e) return 0;
    /* What ends a sentence is not the URL's, nor a closing bracket it
     * did not open. */
    int opens = 0;
    for (int i = start; i <= e; i++) opens += row[i] == '(' ? 1 : row[i] == ')' ? -1 : 0;
    while (e > after && (strchr(".,;:!?'\"", (int)row[e]) || (row[e] == ')' && opens < 0))) {
      if (row[e] == ')') opens++;
      e--;
    }
    if (x > e) return 0;
    a = start;
    z = e;
    for (int i = a; i <= z; i++) {
      if (row[i] == 0) continue;
      uint8_t o[4];
      buf_put(&uri, o, utf8(row[i], o));
    }
  }
  if (uri.failed || uri.len == 0) {
    buf_free(&uri);
    return 0;
  }
  uint8_t *p = ghostty_alloc(NULL, uri.len);
  if (p == NULL) {
    buf_free(&uri);
    return 0;
  }
  memcpy(p, uri.p, uri.len);
  *out = p;
  *len = uri.len;
  *x0 = (uint16_t)a;
  *x1 = (uint16_t)z;
  buf_free(&uri);
  return 1;
}

void inua_vt_scroll(inua_vt *t, uint32_t kind, int32_t n) {
  GhosttyTerminalScrollViewport s = {0};
  switch (kind) {
  case 0: s.tag = GHOSTTY_SCROLL_VIEWPORT_TOP; break;
  case 1: s.tag = GHOSTTY_SCROLL_VIEWPORT_BOTTOM; break;
  case 2: s.tag = GHOSTTY_SCROLL_VIEWPORT_DELTA; s.value.delta = n; break;
  default: s.tag = GHOSTTY_SCROLL_VIEWPORT_DELTA_PROMPT; s.value.delta_prompt = n; break;
  }
  ghostty_terminal_scroll_viewport(t->term, s);
}

/* --- search --- */

int inua_vt_search(inua_vt *t, const uint8_t *needle, size_t len) {
  if (t->search == NULL) {
    GhosttyResult r = ghostty_search_new(NULL, &t->search, t->term);
    if (r != GHOSTTY_SUCCESS) return r;
  }
  GhosttyString s = {needle, len};
  GhosttyResult r = ghostty_search_set(t->search, GHOSTTY_SEARCH_OPT_NEEDLE, len ? &s : NULL);
  t->searching = r == GHOSTTY_SUCCESS && len > 0;
  t->search_changed = true;
  return r;
}

int inua_vt_search_move(inua_vt *t, int32_t dir) {
  if (!t->searching) return 0;
  GhosttyResult r = ghostty_search_set(t->search, dir >= 0 ? GHOSTTY_SEARCH_OPT_SELECT_NEXT : GHOSTTY_SEARCH_OPT_SELECT_PREV, NULL);
  return r == GHOSTTY_SUCCESS;
}

void inua_vt_search_count(inua_vt *t, uint32_t *total, uint32_t *current) {
  *total = *current = 0;
  if (!t->searching) return;
  ghostty_search_run(t->search);
  size_t n = 0, i = 0;
  if (ghostty_search_get(t->search, GHOSTTY_SEARCH_DATA_TOTAL_MATCHES, &n) == GHOSTTY_SUCCESS) *total = (uint32_t)n;
  if (ghostty_search_get(t->search, GHOSTTY_SEARCH_DATA_SELECTED_INDEX, &i) == GHOSTTY_SUCCESS) *current = (uint32_t)i + 1;
}

/* --- writing back to the program --- */

static void out_write(GhosttyTerminal term, void *userdata, const uint8_t *data, size_t len) {
  (void)term;
  buf_put(&((inua_vt *)userdata)->out, data, len);
}

/* Keeps the text of a program's clipboard write: its first text
 * representation. Other kinds (images through OSC 5522) are refused. */
static void clipboard_write(GhosttyTerminal term, void *userdata, const GhosttyClipboardWrite *w) {
  (void)term;
  inua_vt *t = userdata;
  GhosttyClipboardWriteReply reply = {.size = sizeof(reply), .result = GHOSTTY_CLIPBOARD_WRITE_RESULT_UNSUPPORTED};
  for (size_t i = 0; i < w->contents_len; i++) {
    GhosttyString m = w->contents[i].mime;
    if (m.len >= 5 && memcmp(m.ptr, "text/", 5) == 0) {
      t->clip.len = 0;
      t->clip.failed = false;
      buf_put(&t->clip, w->contents[i].data.ptr, w->contents[i].data.len);
      t->clip_at = w->location == GHOSTTY_CLIPBOARD_LOCATION_STANDARD ? 1 : 2;
      reply.result = t->clip.failed ? GHOSTTY_CLIPBOARD_WRITE_RESULT_IO_ERROR : GHOSTTY_CLIPBOARD_WRITE_RESULT_SUCCESS;
      if (t->clip.failed) t->clip_at = 0;
      break;
    }
  }
  w->reply(w, &reply);
}

/* utf8 puts a code point as UTF-8; the bytes written. */
static size_t utf8(uint32_t c, uint8_t *o) {
  if (c < 0x80) { o[0] = (uint8_t)c; return 1; }
  if (c < 0x800) { o[0] = 0xc0 | c >> 6; o[1] = 0x80 | (c & 0x3f); return 2; }
  if (c < 0x10000) { o[0] = 0xe0 | c >> 12; o[1] = 0x80 | (c >> 6 & 0x3f); o[2] = 0x80 | (c & 0x3f); return 3; }
  o[0] = 0xf0 | c >> 18; o[1] = 0x80 | (c >> 12 & 0x3f); o[2] = 0x80 | (c >> 6 & 0x3f); o[3] = 0x80 | (c & 0x3f);
  return 4;
}

/* input_text writes the input cells after the last prompt into b; with
 * at_prompt, only while the cursor is at it (when a command starts, the
 * shell has moved it to the next line already). */
static void input_text(inua_vt *t, buf *b, bool at_prompt) {
  bool at = false;
  if (at_prompt && (ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_CURSOR_AT_PROMPT, &at) != GHOSTTY_SUCCESS || !at)) return;
  uint16_t cols = 0, rows = 0;
  ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_COLS, &cols);
  ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_ROWS, &rows);
  /* The last primary prompt in the active area. */
  int start = -1;
  for (int y = rows - 1; y >= 0 && start < 0; y--) {
    GhosttyGridRef ref = GHOSTTY_INIT_SIZED(GhosttyGridRef);
    GhosttyPoint pt = {.tag = GHOSTTY_POINT_TAG_ACTIVE, .value = {.coordinate = {.x = 0, .y = (uint32_t)y}}};
    if (ghostty_terminal_grid_ref(t->term, pt, &ref) != GHOSTTY_SUCCESS) continue;
    GhosttyRow row = 0;
    GhosttyRowSemanticPrompt sp = GHOSTTY_ROW_SEMANTIC_NONE;
    if (ghostty_grid_ref_row(&ref, &row) == GHOSTTY_SUCCESS &&
        ghostty_row_get(row, GHOSTTY_ROW_DATA_SEMANTIC_PROMPT, &sp) == GHOSTTY_SUCCESS && sp == GHOSTTY_ROW_SEMANTIC_PROMPT)
      start = y;
  }
  if (start < 0) return;
  /* Typing starts where the input mark was, when it is on these rows. */
  int from_x = 0, from_y = start;
  GhosttyPointCoordinate mark;
  if (t->input_at && ghostty_tracked_grid_ref_point(t->input_at, GHOSTTY_POINT_TAG_ACTIVE, &mark) == GHOSTTY_SUCCESS &&
      (int)mark.y >= start && (int)mark.y < rows) {
    from_x = mark.x;
    from_y = (int)mark.y;
  }
  start = from_y;
  size_t line_start = b->len;
  bool any = false;
  for (int y = start; y < rows; y++) {
    bool wrapped = false;
    for (uint16_t x = 0; x < cols; x++) {
      GhosttyGridRef ref = GHOSTTY_INIT_SIZED(GhosttyGridRef);
      GhosttyPoint pt = {.tag = GHOSTTY_POINT_TAG_ACTIVE, .value = {.coordinate = {.x = x, .y = (uint32_t)y}}};
      if (ghostty_terminal_grid_ref(t->term, pt, &ref) != GHOSTTY_SUCCESS) continue;
      if (y == from_y && x < from_x) {
        if (x == 0) {
          GhosttyRow row = 0;
          if (ghostty_grid_ref_row(&ref, &row) == GHOSTTY_SUCCESS) ghostty_row_get(row, GHOSTTY_ROW_DATA_WRAP, &wrapped);
        }
        continue;
      }
      if (x == 0) {
        GhosttyRow row = 0;
        if (ghostty_grid_ref_row(&ref, &row) == GHOSTTY_SUCCESS) ghostty_row_get(row, GHOSTTY_ROW_DATA_WRAP, &wrapped);
      }
      GhosttyCell cell = 0;
      if (ghostty_grid_ref_cell(&ref, &cell) != GHOSTTY_SUCCESS) continue;
      GhosttyCellSemanticContent sc = GHOSTTY_CELL_SEMANTIC_OUTPUT;
      ghostty_cell_get(cell, GHOSTTY_CELL_DATA_SEMANTIC_CONTENT, &sc);
      GhosttyCellWide wide = GHOSTTY_CELL_WIDE_NARROW;
      ghostty_cell_get(cell, GHOSTTY_CELL_DATA_WIDE, &wide);
      if (sc != GHOSTTY_CELL_SEMANTIC_INPUT || wide == GHOSTTY_CELL_WIDE_SPACER_TAIL) continue;
      uint32_t cps[16];
      size_t n = 0;
      if (ghostty_grid_ref_graphemes(&ref, cps, 16, &n) != GHOSTTY_SUCCESS || n == 0) {
        cps[0] = ' ';
        n = 1;
      }
      for (size_t i = 0; i < n; i++) {
        uint8_t o[4];
        buf_put(b, o, utf8(cps[i], o));
      }
      any = true;
    }
    if (!wrapped) {
      /* A line of the command: its trailing blanks go. */
      while (b->len > line_start && b->p[b->len - 1] == ' ') b->len--;
      if (y < rows - 1) buf_put(b, (const uint8_t *)"\n", 1);
      line_start = b->len;
    }
  }
  while (b->len > 0 && (b->p[b->len - 1] == '\n' || b->p[b->len - 1] == ' ')) b->len--;
  if (!any) b->len = 0;
}

int inua_vt_tail(inua_vt *t, uint16_t n, uint8_t **out, size_t *len) {
  *out = NULL;
  *len = 0;
  uint16_t cols = 0, rows = 0;
  ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_COLS, &cols);
  ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_ROWS, &rows);
  buf b = {0};
  for (int y = rows > n ? rows - n : 0; y < rows; y++) {
    size_t line = b.len;
    for (uint16_t x = 0; x < cols; x++) {
      GhosttyGridRef ref = GHOSTTY_INIT_SIZED(GhosttyGridRef);
      GhosttyPoint pt = {.tag = GHOSTTY_POINT_TAG_ACTIVE, .value = {.coordinate = {.x = x, .y = (uint32_t)y}}};
      if (ghostty_terminal_grid_ref(t->term, pt, &ref) != GHOSTTY_SUCCESS) continue;
      GhosttyCell cell = 0;
      if (ghostty_grid_ref_cell(&ref, &cell) != GHOSTTY_SUCCESS) continue;
      GhosttyCellWide wide = GHOSTTY_CELL_WIDE_NARROW;
      ghostty_cell_get(cell, GHOSTTY_CELL_DATA_WIDE, &wide);
      if (wide == GHOSTTY_CELL_WIDE_SPACER_TAIL) continue;
      uint32_t cps[16];
      size_t k = 0;
      if (ghostty_grid_ref_graphemes(&ref, cps, 16, &k) != GHOSTTY_SUCCESS || k == 0) {
        cps[0] = ' ';
        k = 1;
      }
      for (size_t i = 0; i < k; i++) {
        uint8_t o[4];
        buf_put(&b, o, utf8(cps[i], o));
      }
    }
    while (b.len > line && b.p[b.len - 1] == ' ') b.len--;
    if (y < rows - 1) buf_put(&b, (const uint8_t *)"\n", 1);
  }
  if (b.failed) {
    buf_free(&b);
    return GHOSTTY_OUT_OF_MEMORY;
  }
  if (b.len > 0) {
    uint8_t *p = ghostty_alloc(NULL, b.len);
    if (p == NULL) {
      buf_free(&b);
      return GHOSTTY_OUT_OF_MEMORY;
    }
    memcpy(p, b.p, b.len);
    *out = p;
    *len = b.len;
  }
  buf_free(&b);
  return GHOSTTY_SUCCESS;
}

int inua_vt_input(inua_vt *t, uint8_t **out, size_t *len) {
  *out = NULL;
  *len = 0;
  buf b = {0};
  input_text(t, &b, true);
  if (b.failed) {
    buf_free(&b);
    return GHOSTTY_OUT_OF_MEMORY;
  }
  if (b.len == 0) {
    buf_free(&b);
    return GHOSTTY_SUCCESS;
  }
  uint8_t *p = ghostty_alloc(NULL, b.len);
  if (p == NULL) {
    buf_free(&b);
    return GHOSTTY_OUT_OF_MEMORY;
  }
  memcpy(p, b.p, b.len);
  *out = p;
  *len = b.len;
  buf_free(&b);
  return GHOSTTY_SUCCESS;
}

/* Keeps a prompt mark; a command's start takes what was typed when the
 * shell sent no command line. */
static void semantic_prompt(GhosttyTerminal term, void *userdata, const GhosttyTerminalSemanticPrompt *e) {
  (void)term;
  inua_vt *t = userdata;
  uint8_t head[8];
  int32_t code = e->has_exit_code ? e->exit_code : 0;
  head[0] = (uint8_t)e->kind;
  head[1] = e->has_exit_code;
  memcpy(head + 2, &code, 4);
  if (e->kind == GHOSTTY_SEMANTIC_PROMPT_PROMPT_START) t->primary = e->prompt_kind == GHOSTTY_SEMANTIC_PROMPT_PROMPT_PRIMARY;
  if (e->kind == GHOSTTY_SEMANTIC_PROMPT_INPUT_START && t->primary) {
    uint16_t cx = 0, cy = 0;
    ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_CURSOR_X, &cx);
    ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_CURSOR_Y, &cy);
    GhosttyPoint pt = {.tag = GHOSTTY_POINT_TAG_ACTIVE, .value = {.coordinate = {.x = cx, .y = cy}}};
    if (t->input_at == NULL) ghostty_terminal_grid_ref_track(t->term, pt, &t->input_at);
    else ghostty_tracked_grid_ref_set(t->input_at, t->term, pt);
  }
  buf cmd = {0};
  if (e->kind == GHOSTTY_SEMANTIC_PROMPT_OUTPUT_START) {
    if (e->command.len > 0) buf_put(&cmd, e->command.ptr, e->command.len);
    else input_text(t, &cmd, false);
  }
  uint16_t n = cmd.failed ? 0 : (uint16_t)(cmd.len > 65535 ? 65535 : cmd.len);
  memcpy(head + 6, &n, 2);
  buf_put(&t->prompts, head, 8);
  if (n) buf_put(&t->prompts, cmd.p, n);
  buf_free(&cmd);
}

static void desktop_notification(GhosttyTerminal term, void *userdata, const GhosttyTerminalDesktopNotification *n) {
  (void)term;
  inua_vt *t = userdata;
  uint16_t tl = (uint16_t)(n->title.len > 4096 ? 4096 : n->title.len);
  uint16_t bl = (uint16_t)(n->body.len > 4096 ? 4096 : n->body.len);
  buf_put(&t->notes, (const uint8_t *)&tl, 2);
  if (tl) buf_put(&t->notes, n->title.ptr, tl);
  buf_put(&t->notes, (const uint8_t *)&bl, 2);
  if (bl) buf_put(&t->notes, n->body.ptr, bl);
}

void inua_vt_notifications(inua_vt *t, const uint8_t **ptr, size_t *len) {
  *ptr = t->notes.p;
  *len = t->notes.failed ? 0 : t->notes.len;
  t->notes.len = 0;
  t->notes.failed = false;
}

void inua_vt_prompts(inua_vt *t, const uint8_t **ptr, size_t *len) {
  *ptr = t->prompts.p;
  *len = t->prompts.failed ? 0 : t->prompts.len;
  t->prompts.len = 0;
  t->prompts.failed = false;
}

int inua_vt_clipboard(inua_vt *t, const uint8_t **ptr, size_t *len) {
  int at = t->clip_at;
  *ptr = t->clip.p;
  *len = at ? t->clip.len : 0;
  t->clip_at = 0;
  return at;
}

/* As Ghostty answers: a VT220 with colour text and clipboard writes. */
static bool answer_attributes(GhosttyTerminal term, void *userdata, GhosttyDeviceAttributes *out) {
  (void)term;
  (void)userdata;
  out->primary.conformance_level = 62;
  out->primary.features[0] = 22;
  out->primary.features[1] = 52;
  out->primary.num_features = 2;
  out->secondary.device_type = 1;
  out->secondary.firmware_version = 10;
  out->secondary.rom_cartridge = 0;
  out->tertiary.unit_id = 0;
  return true;
}

static GhosttyString answer_version(GhosttyTerminal term, void *userdata) {
  (void)term;
  (void)userdata;
  return (GhosttyString){(const uint8_t *)"inua-term", 9};
}

static bool answer_size(GhosttyTerminal term, void *userdata, GhosttySizeReportSize *out) {
  inua_vt *t = userdata;
  ghostty_terminal_get(term, GHOSTTY_TERMINAL_DATA_COLS, &out->columns);
  ghostty_terminal_get(term, GHOSTTY_TERMINAL_DATA_ROWS, &out->rows);
  out->cell_width = t->cell_w;
  out->cell_height = t->cell_h;
  return true;
}

static bool answer_scheme(GhosttyTerminal term, void *userdata, GhosttyColorScheme *out) {
  (void)term;
  inua_vt *t = userdata;
  if (!t->colored) return false;
  *out = t->dark ? GHOSTTY_COLOR_SCHEME_DARK : GHOSTTY_COLOR_SCHEME_LIGHT;
  return true;
}

void inua_vt_answer(inua_vt *t) {
  t->answering = true;
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_USERDATA, t);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_WRITE_PTY, (const void *)out_write);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_DEVICE_ATTRIBUTES, (const void *)answer_attributes);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_XTVERSION, (const void *)answer_version);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_SIZE, (const void *)answer_size);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_COLOR_SCHEME, (const void *)answer_scheme);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_CLIPBOARD_WRITE, (const void *)clipboard_write);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_SEMANTIC_PROMPT, (const void *)semantic_prompt);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_DESKTOP_NOTIFICATION, (const void *)desktop_notification);
}

void inua_vt_answers(inua_vt *t, const uint8_t **ptr, size_t *len) {
  *ptr = t->out.p;
  *len = t->out.failed ? 0 : t->out.len;
  t->out.len = 0;
  t->out.failed = false;
}

static GhosttyColorRgb color(uint32_t v) {
  return (GhosttyColorRgb){.r = v >> 16 & 0xff, .g = v >> 8 & 0xff, .b = v & 0xff};
}

void inua_vt_colors(inua_vt *t, uint32_t fg, uint32_t bg, uint32_t cursor) {
  GhosttyColorRgb f = color(fg), b = color(bg), c = color(cursor);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_COLOR_FOREGROUND, &f);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_COLOR_BACKGROUND, &b);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_COLOR_CURSOR, &c);
  /* Rec. 709 luma, as programs judge a background dark. */
  t->dark = 2126 * b.r + 7152 * b.g + 722 * b.b < 10000 * 128;
  t->colored = true;
}

void inua_vt_palette(inua_vt *t, const uint32_t *colors16) {
  GhosttyColorRgb pal[256];
  if (ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_COLOR_PALETTE_DEFAULT, pal) != GHOSTTY_SUCCESS) return;
  for (int i = 0; i < 16; i++) pal[i] = color(colors16[i]);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_COLOR_PALETTE, pal);
}

void inua_vt_cell_size(inua_vt *t, uint32_t width, uint32_t height) {
  t->cell_w = width;
  t->cell_h = height;
}

int inua_vt_resize(inua_vt *t, uint16_t cols, uint16_t rows) {
  return ghostty_terminal_resize(t->term, cols, rows, 0, 0);
}

int inua_vt_text(inua_vt *t, int unwrap, uint8_t **out, size_t *len) {
  GhosttyFormatterTerminalOptions opts = GHOSTTY_INIT_SIZED(GhosttyFormatterTerminalOptions);
  opts.emit = GHOSTTY_FORMATTER_FORMAT_PLAIN;
  opts.trim = true;
  opts.unwrap = unwrap != 0;
  GhosttyFormatter f;
  GhosttyResult r = ghostty_formatter_terminal_new(NULL, &f, t->term, opts);
  if (r != GHOSTTY_SUCCESS) return r;
  r = ghostty_formatter_format_alloc(f, NULL, out, len);
  ghostty_formatter_free(f);
  return r;
}

/* The formatter's VT output with these extras, in a buffer of
 * libghostty's. */
static GhosttyResult format_vt(inua_vt *t, GhosttyFormatterTerminalExtra extra, uint8_t **out, size_t *len) {
  GhosttyFormatterTerminalOptions opts = GHOSTTY_INIT_SIZED(GhosttyFormatterTerminalOptions);
  opts.emit = GHOSTTY_FORMATTER_FORMAT_VT;
  opts.extra = extra;
  GhosttyFormatter f;
  GhosttyResult r = ghostty_formatter_terminal_new(NULL, &f, t->term, opts);
  if (r != GHOSTTY_SUCCESS) return r;
  r = ghostty_formatter_format_alloc(f, NULL, out, len);
  ghostty_formatter_free(f);
  return r;
}

int inua_vt_snapshot(inua_vt *t, uint8_t **out, size_t *len) {
  *out = NULL;
  *len = 0;
  /* The modes, then every row as a line of its own: the formatter
   * writes a row's end as \r\n, and the blank rows at the end not at
   * all. */
  GhosttyFormatterTerminalExtra plain = GHOSTTY_INIT_SIZED(GhosttyFormatterTerminalExtra);
  plain.modes = true;
  plain.screen = (GhosttyFormatterScreenExtra)GHOSTTY_INIT_SIZED(GhosttyFormatterScreenExtra);
  /* The same with the state that comes after the rows: the scrolling
   * region, the keyboard's modes, the character sets, the style. */
  GhosttyFormatterTerminalExtra full = plain;
  full.scrolling_region = true;
  full.keyboard = true;
  full.screen.style = true;
  full.screen.kitty_keyboard = true;
  full.screen.charsets = true;
  uint8_t *a = NULL, *b = NULL;
  size_t alen = 0, blen = 0;
  GhosttyResult r = format_vt(t, plain, &a, &alen);
  if (r == GHOSTTY_SUCCESS) r = format_vt(t, full, &b, &blen);
  if (r != GHOSTTY_SUCCESS) {
    if (a) ghostty_free(NULL, a, alen);
    if (b) ghostty_free(NULL, b, blen);
    return r;
  }
  /* The rows written, and the rows the screen has with its scrollback:
   * as many line ends again put the last row where it is here, and
   * where the screen starts. */
  size_t written = 0, total = 0;
  bool any = false;
  for (size_t i = 0; i < alen; i++) {
    if (a[i] == '\n' && i > 0 && a[i - 1] == '\r') written++;
    else if (a[i] != '\r') any = true;
  }
  if (any) written++;
  ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_TOTAL_ROWS, &total);
  uint16_t cx = 0, cy = 0;
  ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_CURSOR_X, &cx);
  ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_CURSOR_Y, &cy);

  buf o = {0};
  buf_put(&o, a, alen);
  for (size_t i = written ? written : 1; i < total; i++) buf_put(&o, (const uint8_t *)"\r\n", 2);
  /* What the second pass wrote after the first: the state after the
   * rows, set once they are there. Should the second not begin with the
   * first, the state is left out. */
  if (blen >= alen && memcmp(a, b, alen) == 0) buf_put(&o, b + alen, blen - alen);
  char cup[24];
  int n = snprintf(cup, sizeof cup, "\x1b[%u;%uH", (unsigned)cy + 1, (unsigned)cx + 1);
  buf_put(&o, (const uint8_t *)cup, (size_t)n);
  ghostty_free(NULL, a, alen);
  ghostty_free(NULL, b, blen);
  if (o.failed) {
    buf_free(&o);
    return GHOSTTY_OUT_OF_MEMORY;
  }
  uint8_t *p = ghostty_alloc(NULL, o.len);
  if (p == NULL) {
    buf_free(&o);
    return GHOSTTY_OUT_OF_MEMORY;
  }
  memcpy(p, o.p, o.len);
  *out = p;
  *len = o.len;
  buf_free(&o);
  return GHOSTTY_SUCCESS;
}

void inua_vt_free_bytes(uint8_t *p, size_t len) { ghostty_free(NULL, p, len); }

static int get_string(inua_vt *t, GhosttyTerminalData what, const uint8_t **ptr, size_t *len) {
  GhosttyString s = {0};
  GhosttyResult r = ghostty_terminal_get(t->term, what, &s);
  *ptr = s.ptr;
  *len = s.len;
  return r;
}

int inua_vt_title(inua_vt *t, const uint8_t **ptr, size_t *len) {
  return get_string(t, GHOSTTY_TERMINAL_DATA_TITLE, ptr, len);
}

int inua_vt_pwd(inua_vt *t, const uint8_t **ptr, size_t *len) {
  return get_string(t, GHOSTTY_TERMINAL_DATA_PWD, ptr, len);
}

int inua_vt_cursor(inua_vt *t, uint16_t *x, uint16_t *y) {
  GhosttyResult r = ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_CURSOR_X, x);
  if (r != GHOSTTY_SUCCESS) return r;
  return ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_CURSOR_Y, y);
}

/* --- frames --- */

static uint32_t rgb(GhosttyColorRgb c) { return (uint32_t)c.r << 16 | (uint32_t)c.g << 8 | c.b; }

typedef struct {
  uint8_t *buf;
  size_t cap, len;
  bool full;
} out_buf;

static void put(out_buf *o, const void *p, size_t n) {
  if (o->len + n > o->cap) {
    o->full = true;
    return;
  }
  memcpy(o->buf + o->len, p, n);
  o->len += n;
}

static void put16(out_buf *o, uint16_t v) { put(o, &v, 2); }
static void put32(out_buf *o, uint32_t v) { put(o, &v, 4); }

/* A run being gathered for one row. */
typedef struct {
  uint16_t x, cells;
  uint32_t fg, bg;
  uint16_t attrs;
  uint8_t text[4096];
  size_t len;
} run;

static void put_run(out_buf *o, const run *r) {
  put16(o, r->x);
  put16(o, r->cells);
  put32(o, r->fg);
  put32(o, r->bg);
  put16(o, r->attrs);
  put16(o, (uint16_t)r->len);
  put(o, r->text, r->len);
}

/* One row's runs; returns how many were written. The row's last run
 * loses its trailing spaces when they draw nothing (default colours, no
 * attributes), and is left out when nothing else is in it. */
/* in says cell x, y is inside s. */
static bool in(const span *s, uint16_t x, uint32_t y) {
  if (y < s->sy || y > s->ey) return false;
  if (y == s->sy && x < s->sx) return false;
  if (y == s->ey && x > s->ex) return false;
  return true;
}

/* to_span is a match in viewport cells; false when it is not in view. */
static bool to_span(inua_vt *t, const GhosttySelection *sel, span *out) {
  GhosttyPointCoordinate a, b;
  if (ghostty_terminal_point_from_grid_ref(t->term, &sel->start, GHOSTTY_POINT_TAG_VIEWPORT, &a) != GHOSTTY_SUCCESS) return false;
  if (ghostty_terminal_point_from_grid_ref(t->term, &sel->end, GHOSTTY_POINT_TAG_VIEWPORT, &b) != GHOSTTY_SUCCESS) return false;
  if (b.y < a.y || (b.y == a.y && b.x < a.x)) {
    GhosttyPointCoordinate s = a;
    a = b;
    b = s;
  }
  *out = (span){.sx = a.x, .sy = a.y, .ex = b.x, .ey = b.y};
  return true;
}

/* find_spans catches the search up and reads the matches in view. */
static void find_spans(inua_vt *t) {
  t->nspans = 0;
  t->has_current = false;
  if (!t->searching) return;
  ghostty_search_run(t->search);
  GhosttySelection sels[MAX_SPANS];
  GhosttySelectionBuffer b = {.ptr = sels, .cap = MAX_SPANS};
  for (size_t i = 0; i < MAX_SPANS; i++) sels[i] = (GhosttySelection)GHOSTTY_INIT_SIZED(GhosttySelection);
  GhosttyResult r = ghostty_search_get(t->search, GHOSTTY_SEARCH_DATA_VIEWPORT_MATCHES, &b);
  size_t n = r == GHOSTTY_SUCCESS ? b.len : 0;
  for (size_t i = 0; i < n; i++)
    if (to_span(t, &sels[i], &t->spans[t->nspans])) t->nspans++;
  GhosttySelection cur = GHOSTTY_INIT_SIZED(GhosttySelection);
  if (ghostty_search_get(t->search, GHOSTTY_SEARCH_DATA_SELECTED_MATCH, &cur) == GHOSTTY_SUCCESS)
    t->has_current = to_span(t, &cur, &t->current);
}

static uint16_t match_attrs(inua_vt *t, uint16_t x, uint16_t y) {
  if (t->has_current && in(&t->current, x, y)) return INUA_VT_MATCH | INUA_VT_MATCH_CURRENT;
  for (size_t i = 0; i < t->nspans; i++)
    if (in(&t->spans[i], x, y)) return INUA_VT_MATCH;
  return 0;
}

static uint16_t put_row(inua_vt *t, out_buf *o, uint16_t y) {
  run cur = {0};
  bool open = false;
  uint16_t nruns = 0, x = 0;
  if (ghostty_render_state_row_get(t->rows, GHOSTTY_RENDER_STATE_ROW_DATA_CELLS, &t->cells) != GHOSTTY_SUCCESS)
    return 0;
  GhosttyRenderStateRowSelection sel = GHOSTTY_INIT_SIZED(GhosttyRenderStateRowSelection);
  bool has_sel = ghostty_render_state_row_get(t->rows, GHOSTTY_RENDER_STATE_ROW_DATA_SELECTION, &sel) == GHOSTTY_SUCCESS;
  while (ghostty_render_state_row_cells_next(t->cells)) {
    GhosttyCell raw = 0;
    ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_RAW, &raw);
    GhosttyCellWide wide = GHOSTTY_CELL_WIDE_NARROW;
    ghostty_cell_get(raw, GHOSTTY_CELL_DATA_WIDE, &wide);
    if (wide == GHOSTTY_CELL_WIDE_SPACER_TAIL) { /* the wide character's run counts it */
      if (open) cur.cells++;
      x++;
      continue;
    }
    uint32_t fg = INUA_VT_DEFAULT_COLOR, bg = INUA_VT_DEFAULT_COLOR;
    GhosttyColorRgb c;
    if (ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_FG_COLOR, &c) == GHOSTTY_SUCCESS) fg = rgb(c);
    if (ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_BG_COLOR, &c) == GHOSTTY_SUCCESS) bg = rgb(c);
    uint16_t attrs = 0;
    bool styled = false;
    ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_HAS_STYLING, &styled);
    if (styled) {
      GhosttyStyle st = GHOSTTY_INIT_SIZED(GhosttyStyle);
      ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE, &st);
      attrs |= st.bold ? INUA_VT_BOLD : 0;
      attrs |= st.italic ? INUA_VT_ITALIC : 0;
      attrs |= st.faint ? INUA_VT_FAINT : 0;
      attrs |= st.inverse ? INUA_VT_INVERSE : 0;
      attrs |= st.strikethrough ? INUA_VT_STRIKE : 0;
      attrs |= st.underline != 0 ? INUA_VT_UNDERLINE : 0;
      attrs |= st.invisible ? INUA_VT_INVISIBLE : 0;
      attrs |= st.overline ? INUA_VT_OVERLINE : 0;
    }
    if (has_sel && x >= sel.start_x && x <= sel.end_x) attrs |= INUA_VT_SELECTED;
    if (t->searching) attrs |= match_attrs(t, x, y);
    uint8_t text[64];
    GhosttyBuffer gb = {.ptr = text, .cap = sizeof(text), .len = 0};
    if (ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_UTF8, &gb) != GHOSTTY_SUCCESS || gb.len == 0) {
      text[0] = ' ';
      gb.len = 1;
    }
    if (!open || cur.fg != fg || cur.bg != bg || cur.attrs != attrs || cur.len + gb.len > sizeof(cur.text)) {
      if (open) {
        put_run(o, &cur);
        nruns++;
      }
      memset(&cur, 0, sizeof(cur));
      cur.x = x;
      cur.fg = fg;
      cur.bg = bg;
      cur.attrs = attrs;
      open = true;
    }
    memcpy(cur.text + cur.len, text, gb.len);
    cur.len += gb.len;
    cur.cells++;
    x++;
  }
  if (open && cur.fg == INUA_VT_DEFAULT_COLOR && cur.bg == INUA_VT_DEFAULT_COLOR && cur.attrs == 0) {
    while (cur.len > 0 && cur.text[cur.len - 1] == ' ') {
      cur.len--;
      cur.cells--;
    }
  }
  if (open && cur.len > 0) {
    put_run(o, &cur);
    nruns++;
  }
  return nruns;
}

int inua_vt_frame(inua_vt *t, int all, uint8_t *buf, size_t cap, size_t *len, inua_vt_frame_info *info) {
  GhosttyResult r = ghostty_render_state_update(t->render, t->term);
  if (r != GHOSTTY_SUCCESS) return r;
  memset(info, 0, sizeof(*info));
  uint16_t cols = 0, rows = 0;
  ghostty_render_state_get(t->render, GHOSTTY_RENDER_STATE_DATA_COLS, &cols);
  ghostty_render_state_get(t->render, GHOSTTY_RENDER_STATE_DATA_ROWS, &rows);
  info->cols = cols;
  info->rows = rows;
  GhosttyRenderStateDirty dirty = GHOSTTY_RENDER_STATE_DIRTY_FALSE;
  ghostty_render_state_get(t->render, GHOSTTY_RENDER_STATE_DATA_DIRTY, &dirty);
  info->dirty = (uint32_t)dirty;
  GhosttyRenderStateCursor cur = GHOSTTY_INIT_SIZED(GhosttyRenderStateCursor);
  if (ghostty_render_state_get(t->render, GHOSTTY_RENDER_STATE_DATA_CURSOR, &cur) == GHOSTTY_SUCCESS) {
    info->cursor_visible = cur.visible && cur.viewport_has_value;
    info->cursor_x = cur.viewport_x;
    info->cursor_y = cur.viewport_y;
    info->cursor_style = (uint32_t)cur.visual_style;
    info->cursor_wide = cur.wide_tail;
    info->password = cur.password_input;
    info->cursor_blink = cur.blinking;
  }
  GhosttyRenderStateColors colors = GHOSTTY_INIT_SIZED(GhosttyRenderStateColors);
  if (ghostty_render_state_get(t->render, GHOSTTY_RENDER_STATE_DATA_COLORS, &colors) == GHOSTTY_SUCCESS) {
    info->fg = rgb(colors.foreground);
    info->bg = rgb(colors.background);
  }
  bool mouse = false;
  ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_MOUSE_TRACKING, &mouse);
  info->mouse = mouse;
  GhosttyTerminalScreen screen = GHOSTTY_TERMINAL_SCREEN_PRIMARY;
  ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_ACTIVE_SCREEN, &screen);
  info->alt = screen == GHOSTTY_TERMINAL_SCREEN_ALTERNATE;
  GhosttyTerminalScrollbar sb = {0};
  if (ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_SCROLLBAR, &sb) == GHOSTTY_SUCCESS) {
    info->sb_total = (uint32_t)sb.total;
    info->sb_offset = (uint32_t)sb.offset;
    info->sb_len = (uint32_t)sb.len;
  }
  /* While searching every row is drawn: matches move with output and
   * scrolling; once more when the search ends, to take them away. */
  find_spans(t);
  bool every = all || dirty == GHOSTTY_RENDER_STATE_DIRTY_FULL || t->searching || t->search_changed;
  t->search_changed = false;
  out_buf o = {.buf = buf, .cap = cap};
  if (ghostty_render_state_get(t->render, GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, &t->rows) != GHOSTTY_SUCCESS)
    return GHOSTTY_INVALID_VALUE;
  uint16_t y = 0;
  for (;;) {
    if (every) {
      if (!ghostty_render_state_row_iterator_next(t->rows)) break;
    } else if (!ghostty_render_state_row_iterator_next_dirty(t->rows, &y)) {
      break;
    }
    if (every) {
      uint16_t vy = 0;
      ghostty_render_state_row_get(t->rows, GHOSTTY_RENDER_STATE_ROW_DATA_VIEWPORT_Y, &vy);
      y = vy;
    }
    size_t head = o.len;
    put16(&o, y);
    put16(&o, 0);
    uint16_t n = put_row(t, &o, y);
    if (!o.full) memcpy(o.buf + head + 2, &n, 2);
    info->nrows++;
    if (o.full) break;
  }
  if (o.full) {
    *len = cap * 2 + 65536;
    return GHOSTTY_OUT_OF_SPACE;
  }
  *len = o.len;
  /* Drawn: everything is clean until the terminal changes again. */
  if (ghostty_render_state_get(t->render, GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, &t->rows) == GHOSTTY_SUCCESS) {
    bool clean = false;
    while (ghostty_render_state_row_iterator_next(t->rows))
      ghostty_render_state_row_set(t->rows, GHOSTTY_RENDER_STATE_ROW_OPTION_DIRTY, &clean);
  }
  GhosttyRenderStateDirty none = GHOSTTY_RENDER_STATE_DIRTY_FALSE;
  ghostty_render_state_set(t->render, GHOSTTY_RENDER_STATE_OPTION_DIRTY, &none);
  return GHOSTTY_SUCCESS;
}

/* --- keys --- */

static const struct {
  uint32_t xkb;
  GhosttyKey key;
} keymap[] = {
#include "inua_keymap.inc"
};

static GhosttyKey key_for_xkb(uint32_t xkb) {
  for (size_t i = 0; i < sizeof(keymap) / sizeof(keymap[0]); i++)
    if (keymap[i].xkb == xkb) return keymap[i].key;
  return GHOSTTY_KEY_UNIDENTIFIED;
}

/* The named keys by keysym (X11's keysymdef.h values). */
static GhosttyKey key_for_keysym(uint32_t sym) {
  static const GhosttyKey f[] = {GHOSTTY_KEY_F1, GHOSTTY_KEY_F2, GHOSTTY_KEY_F3, GHOSTTY_KEY_F4, GHOSTTY_KEY_F5, GHOSTTY_KEY_F6, GHOSTTY_KEY_F7, GHOSTTY_KEY_F8, GHOSTTY_KEY_F9, GHOSTTY_KEY_F10, GHOSTTY_KEY_F11, GHOSTTY_KEY_F12, GHOSTTY_KEY_F13, GHOSTTY_KEY_F14, GHOSTTY_KEY_F15, GHOSTTY_KEY_F16, GHOSTTY_KEY_F17, GHOSTTY_KEY_F18, GHOSTTY_KEY_F19, GHOSTTY_KEY_F20, GHOSTTY_KEY_F21, GHOSTTY_KEY_F22, GHOSTTY_KEY_F23, GHOSTTY_KEY_F24, GHOSTTY_KEY_F25};
  if (sym >= 0xffbe && sym <= 0xffd6) return f[sym - 0xffbe]; /* F1…F25 */
  switch (sym) {
  case 0xff09: case 0xfe20: return GHOSTTY_KEY_TAB; /* Tab, ISO_Left_Tab (Shift+Tab) */
  case 0xffeb: case 0xffe7: return GHOSTTY_KEY_META_LEFT;  /* Super_L, Meta_L */
  case 0xffec: case 0xffe8: return GHOSTTY_KEY_META_RIGHT; /* Super_R, Meta_R */
  case 0xff0d: return GHOSTTY_KEY_ENTER;
  case 0xff08: return GHOSTTY_KEY_BACKSPACE;
  case 0xff1b: return GHOSTTY_KEY_ESCAPE;
  case 0xffff: return GHOSTTY_KEY_DELETE;
  case 0xff63: return GHOSTTY_KEY_INSERT;
  case 0xff50: return GHOSTTY_KEY_HOME;
  case 0xff57: return GHOSTTY_KEY_END;
  case 0xff55: return GHOSTTY_KEY_PAGE_UP;
  case 0xff56: return GHOSTTY_KEY_PAGE_DOWN;
  case 0xff51: return GHOSTTY_KEY_ARROW_LEFT;
  case 0xff52: return GHOSTTY_KEY_ARROW_UP;
  case 0xff53: return GHOSTTY_KEY_ARROW_RIGHT;
  case 0xff54: return GHOSTTY_KEY_ARROW_DOWN;
  case 0xff61: return GHOSTTY_KEY_PRINT_SCREEN;
  case 0xff67: return GHOSTTY_KEY_CONTEXT_MENU;
  case 0xff13: return GHOSTTY_KEY_PAUSE;
  case 0xff14: return GHOSTTY_KEY_SCROLL_LOCK;
  case 0xff7f: return GHOSTTY_KEY_NUM_LOCK;
  case 0xffe5: return GHOSTTY_KEY_CAPS_LOCK;
  case 0xffe1: return GHOSTTY_KEY_SHIFT_LEFT;
  case 0xffe2: return GHOSTTY_KEY_SHIFT_RIGHT;
  case 0xffe3: return GHOSTTY_KEY_CONTROL_LEFT;
  case 0xffe4: return GHOSTTY_KEY_CONTROL_RIGHT;
  case 0xffe9: return GHOSTTY_KEY_ALT_LEFT;
  case 0xffea: return GHOSTTY_KEY_ALT_RIGHT;
  case 0x0020: return GHOSTTY_KEY_SPACE;
  case 0xff8d: return GHOSTTY_KEY_NUMPAD_ENTER;
  case 0xff95: return GHOSTTY_KEY_NUMPAD_HOME;
  case 0xff96: return GHOSTTY_KEY_NUMPAD_LEFT;
  case 0xff97: return GHOSTTY_KEY_NUMPAD_UP;
  case 0xff98: return GHOSTTY_KEY_NUMPAD_RIGHT;
  case 0xff99: return GHOSTTY_KEY_NUMPAD_DOWN;
  case 0xff9a: return GHOSTTY_KEY_NUMPAD_PAGE_UP;
  case 0xff9b: return GHOSTTY_KEY_NUMPAD_PAGE_DOWN;
  case 0xff9c: return GHOSTTY_KEY_NUMPAD_END;
  case 0xff9d: return GHOSTTY_KEY_NUMPAD_BEGIN;
  case 0xff9e: return GHOSTTY_KEY_NUMPAD_INSERT;
  case 0xff9f: return GHOSTTY_KEY_NUMPAD_DELETE;
  case 0xffbd: return GHOSTTY_KEY_NUMPAD_EQUAL;
  case 0xffaa: return GHOSTTY_KEY_NUMPAD_MULTIPLY;
  case 0xffab: return GHOSTTY_KEY_NUMPAD_ADD;
  case 0xffac: return GHOSTTY_KEY_NUMPAD_SEPARATOR;
  case 0xffad: return GHOSTTY_KEY_NUMPAD_SUBTRACT;
  case 0xffae: return GHOSTTY_KEY_NUMPAD_DECIMAL;
  case 0xffaf: return GHOSTTY_KEY_NUMPAD_DIVIDE;
  case 0xffb0: return GHOSTTY_KEY_NUMPAD_0;
  case 0xffb1: return GHOSTTY_KEY_NUMPAD_1;
  case 0xffb2: return GHOSTTY_KEY_NUMPAD_2;
  case 0xffb3: return GHOSTTY_KEY_NUMPAD_3;
  case 0xffb4: return GHOSTTY_KEY_NUMPAD_4;
  case 0xffb5: return GHOSTTY_KEY_NUMPAD_5;
  case 0xffb6: return GHOSTTY_KEY_NUMPAD_6;
  case 0xffb7: return GHOSTTY_KEY_NUMPAD_7;
  case 0xffb8: return GHOSTTY_KEY_NUMPAD_8;
  case 0xffb9: return GHOSTTY_KEY_NUMPAD_9;
  }
  return GHOSTTY_KEY_UNIDENTIFIED;
}

int inua_vt_key(inua_vt *t, uint32_t keysym, uint32_t xkb, uint32_t mods, uint32_t action,
                const char *utf8, size_t utf8_len, uint32_t unshifted,
                uint32_t consumed, uint8_t *out, size_t cap, size_t *len) {
  *len = 0;
  /* A release means something only to a program that asked for them. */
  if (action == GHOSTTY_KEY_ACTION_RELEASE) {
    uint8_t flags = 0;
    ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_KITTY_KEYBOARD_FLAGS, &flags);
    if ((flags & GHOSTTY_KITTY_KEY_REPORT_EVENTS) == 0) return GHOSTTY_SUCCESS;
  }
  ghostty_key_encoder_setopt_from_terminal(t->keys, t->term);
  /* Alt is Alt: on macOS the encoder would otherwise take Option for
   * composing characters and drop it. A client that composes with Option
   * sends the composed text without Alt. */
  GhosttyOptionAsAlt alt = GHOSTTY_OPTION_AS_ALT_TRUE;
  ghostty_key_encoder_setopt(t->keys, GHOSTTY_KEY_ENCODER_OPT_MACOS_OPTION_AS_ALT, &alt);
  ghostty_key_event_set_action(t->key, (GhosttyKeyAction)action);
  GhosttyKey k = key_for_keysym(keysym);
  if (k == GHOSTTY_KEY_UNIDENTIFIED) k = key_for_xkb(xkb);
  ghostty_key_event_set_key(t->key, k);
  ghostty_key_event_set_mods(t->key, (GhosttyMods)mods);
  ghostty_key_event_set_consumed_mods(t->key, (GhosttyMods)consumed);
  ghostty_key_event_set_utf8(t->key, utf8_len ? utf8 : NULL, utf8_len);
  ghostty_key_event_set_unshifted_codepoint(t->key, unshifted);
  ghostty_key_event_set_composing(t->key, false);
  *len = 0;
  return ghostty_key_encoder_encode(t->keys, t->key, (char *)out, cap, len);
}

/* --- mouse --- */

int inua_vt_mouse(inua_vt *t, uint32_t action, uint32_t button, uint32_t mods,
                  double x, double y, uint32_t width, uint32_t height,
                  uint32_t cell_w, uint32_t cell_h, uint32_t pad, uint32_t pressed,
                  uint8_t *out, size_t cap, size_t *len) {
  *len = 0;
  if (cell_w == 0 || cell_h == 0) return GHOSTTY_INVALID_VALUE;
  ghostty_mouse_encoder_setopt_from_terminal(t->mouse, t->term);
  GhosttyMouseEncoderSize size = {
      .size = sizeof(size),
      .screen_width = width,
      .screen_height = height,
      .cell_width = cell_w,
      .cell_height = cell_h,
      .padding_top = pad,
      .padding_bottom = pad,
      .padding_right = pad,
      .padding_left = pad,
  };
  ghostty_mouse_encoder_setopt(t->mouse, GHOSTTY_MOUSE_ENCODER_OPT_SIZE, &size);
  bool any = pressed != 0;
  ghostty_mouse_encoder_setopt(t->mouse, GHOSTTY_MOUSE_ENCODER_OPT_ANY_BUTTON_PRESSED, &any);
  /* Motion is sent once a cell. */
  int32_t cx = x < pad ? 0 : (int32_t)((x - pad) / cell_w);
  int32_t cy = y < pad ? 0 : (int32_t)((y - pad) / cell_h);
  if (action == GHOSTTY_MOUSE_ACTION_MOTION) {
    if (cx == t->mouse_x && cy == t->mouse_y) return GHOSTTY_SUCCESS;
  }
  ghostty_mouse_event_set_action(t->mouse_ev, (GhosttyMouseAction)action);
  if (button) ghostty_mouse_event_set_button(t->mouse_ev, (GhosttyMouseButton)button);
  else ghostty_mouse_event_clear_button(t->mouse_ev);
  ghostty_mouse_event_set_mods(t->mouse_ev, (GhosttyMods)mods);
  ghostty_mouse_event_set_position(t->mouse_ev, (GhosttyMousePosition){.x = (float)x, .y = (float)y});
  GhosttyResult r = ghostty_mouse_encoder_encode(t->mouse, t->mouse_ev, (char *)out, cap, len);
  if (r == GHOSTTY_SUCCESS && *len > 0 && action == GHOSTTY_MOUSE_ACTION_MOTION) {
    t->mouse_x = cx;
    t->mouse_y = cy;
  }
  return r;
}

/* --- selection --- */

int inua_vt_select(inua_vt *t, uint32_t kind, uint16_t x, uint16_t y,
                   double px, double py, uint64_t time_ns, uint64_t repeat_ns,
                   double repeat_px, uint32_t rectangle, uint32_t cell_w,
                   uint32_t height) {
  GhosttySelectionGestureEvent ev = kind == 0 ? t->press : kind == 1 ? t->release : t->drag;
  GhosttyGridRef ref = GHOSTTY_INIT_SIZED(GhosttyGridRef);
  GhosttyPoint pt = {.tag = GHOSTTY_POINT_TAG_VIEWPORT, .value = {.coordinate = {.x = x, .y = y}}};
  GhosttyResult r = ghostty_terminal_grid_ref(t->term, pt, &ref);
  if (r != GHOSTTY_SUCCESS) return r;
  ghostty_selection_gesture_event_set(ev, GHOSTTY_SELECTION_GESTURE_EVENT_OPT_REF, &ref);
  GhosttySurfacePosition pos = {.x = px, .y = py};
  ghostty_selection_gesture_event_set(ev, GHOSTTY_SELECTION_GESTURE_EVENT_OPT_POSITION, &pos);
  bool rect = rectangle != 0;
  ghostty_selection_gesture_event_set(ev, GHOSTTY_SELECTION_GESTURE_EVENT_OPT_RECTANGLE, &rect);
  if (kind == 0) {
    ghostty_selection_gesture_event_set(ev, GHOSTTY_SELECTION_GESTURE_EVENT_OPT_TIME_NS, &time_ns);
    ghostty_selection_gesture_event_set(ev, GHOSTTY_SELECTION_GESTURE_EVENT_OPT_REPEAT_INTERVAL_NS, &repeat_ns);
    ghostty_selection_gesture_event_set(ev, GHOSTTY_SELECTION_GESTURE_EVENT_OPT_REPEAT_DISTANCE, &repeat_px);
  }
  uint16_t cols = 0;
  ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_COLS, &cols);
  GhosttySelectionGestureGeometry geo = {.columns = cols, .cell_width = cell_w, .padding_left = 0, .screen_height = height};
  if (kind == 2) ghostty_selection_gesture_event_set(ev, GHOSTTY_SELECTION_GESTURE_EVENT_OPT_GEOMETRY, &geo);
  GhosttySelection sel = GHOSTTY_INIT_SIZED(GhosttySelection);
  r = ghostty_selection_gesture_event(t->gesture, t->term, ev, kind == 1 ? NULL : &sel);
  if (r == GHOSTTY_SUCCESS && kind != 1) {
    ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_SELECTION, &sel);
    return 1;
  }
  if (r == GHOSTTY_NO_VALUE) {
    if (kind == 0) ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_SELECTION, NULL);
    GhosttySelection cur = GHOSTTY_INIT_SIZED(GhosttySelection);
    return ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_SELECTION, &cur) == GHOSTTY_SUCCESS;
  }
  return r;
}

int inua_vt_selection_text(inua_vt *t, uint8_t **out, size_t *len) {
  *out = NULL;
  *len = 0;
  GhosttySelection sel = GHOSTTY_INIT_SIZED(GhosttySelection);
  if (ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_SELECTION, &sel) != GHOSTTY_SUCCESS) return GHOSTTY_SUCCESS;
  GhosttyTerminalSelectionFormatOptions opts = GHOSTTY_INIT_SIZED(GhosttyTerminalSelectionFormatOptions);
  opts.emit = GHOSTTY_FORMATTER_FORMAT_PLAIN;
  opts.unwrap = true;
  opts.trim = true;
  opts.selection = &sel;
  return ghostty_terminal_selection_format_alloc(t->term, NULL, opts, out, len);
}

void inua_vt_select_clear(inua_vt *t) {
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_SELECTION, NULL);
  ghostty_selection_gesture_reset(t->gesture, t->term);
}

/* --- paste --- */

typedef struct {
  const uint8_t *p;
  size_t len;
} paste_text;

static bool paste_read(void *userdata, GhosttyString mime, GhosttyWriter w) {
  (void)mime;
  paste_text *s = userdata;
  return s->len == 0 || w.write(w.userdata, s->p, s->len);
}

int inua_vt_paste(inua_vt *t, const uint8_t *data, size_t len, uint32_t primary,
                  uint32_t allow_unsafe, uint8_t **out, size_t *out_len) {
  *out = NULL;
  *out_len = 0;
  paste_text text = {data, len};
  GhosttyString mimes[] = {{(const uint8_t *)"text/plain", 10}};
  GhosttyPaste paste = {
      .size = sizeof(paste),
      .location = primary ? GHOSTTY_CLIPBOARD_LOCATION_PRIMARY : GHOSTTY_CLIPBOARD_LOCATION_STANDARD,
      .source = GHOSTTY_PASTE_SOURCE_CLIPBOARD,
      .mimes = mimes,
      .mimes_len = 1,
      .reader = {.read = paste_read, .userdata = &text},
      .allow_unsafe = allow_unsafe != 0,
  };
  /* The paste is written after any answers not yet taken, and cut off
   * them again. A terminal that does not answer has the writer for the
   * paste alone. */
  size_t from = t->out.len;
  if (!t->answering) {
    ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_USERDATA, t);
    ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_WRITE_PTY, (const void *)out_write);
  }
  GhosttyResult r = ghostty_terminal_paste(t->term, &paste, NULL);
  if (!t->answering) ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_WRITE_PTY, NULL);
  size_t n = t->out.len - from;
  t->out.len = from;
  if (r == GHOSTTY_SUCCESS && t->out.failed) r = GHOSTTY_OUT_OF_MEMORY;
  if (!t->answering) t->out.failed = false;
  if (r != GHOSTTY_SUCCESS || n == 0) return r;
  uint8_t *p = ghostty_alloc(NULL, n);
  if (p == NULL) return GHOSTTY_OUT_OF_MEMORY;
  memcpy(p, t->out.p + from, n);
  *out = p;
  *out_len = n;
  return GHOSTTY_SUCCESS;
}
