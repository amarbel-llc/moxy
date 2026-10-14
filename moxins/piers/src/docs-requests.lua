-- A pandoc custom writer that turns a document into Google Docs API
-- batchUpdate requests which write it into an EMPTY body (or tab), starting
-- at index 1. Requests carry no tabId; the caller (docs-markdown.ts) adds
-- one. Proven out in zz-pocs/pandoc-docs-poc (#456, #457, #453).
--
--   pandoc --preserve-tabs -f gfm -t docs-requests.lua < doc.md
--     => {"requests":[...], "footnotes":[{"requests":[...]}, ...]}
--
-- Docs indexes count UTF-16 code units, so every length goes through len16.
-- The last paragraph reuses the body's existing final newline, so the
-- inserted text has one fewer "\n" than there are paragraphs.
--
-- A rendered block list is a *plan*: requests that write it into an empty
-- segment whose insertion point is index 1. A plan is placed elsewhere by
-- shifting every index (table cells, footnotes). Execution order inside a
-- plan:
--
--   1. insertText of all the text, then resets of inherited style
--   2. paragraph and text styles
--   3. bullets, last group first (createParagraphBullets deletes the leading
--      tabs it reads nesting from, shifting later indexes)
--   4. structural ops — tables, images, footnote references — in descending
--      position, so each one leaves the positions of those still to come
--      intact. Positions are mapped through the tab removals of step 3.
--
-- Two things the caller finishes: an insertInlineImage carrying a `pending`
-- field (a local file or PlantUML source) needs its uri filled in, and each
-- createFootnote's content (the matching entry of `footnotes`, in request
-- order) is a plan to apply once the footnote's id is known.

local CODE_FONT = "Roboto Mono"
local CODE_SIZE_PT = 10
local CODE_BG = { red = 0.95, green = 0.95, blue = 0.95 }
local HEADER_BG = { red = 0.9, green = 0.9, blue = 0.9 }
local LINK_FG = { red = 0.07, green = 0.33, blue = 0.8 }
local QUOTE_BORDER = { red = 0.8, green = 0.8, blue = 0.8 }
local RULE_BORDER = { red = 0.6, green = 0.6, blue = 0.6 }
local INDENT_PT = 36
local PARA_SPACE_BELOW_PT = 8
local LINE_BREAK = "\v"
local TASK_OPEN, TASK_DONE = "\u{2610}", "\u{2612}"
local PLANTUML_CLASSES = { plantuml = true, puml = true }
local ALIGNMENT = { AlignLeft = "START", AlignCenter = "CENTER", AlignRight = "END" }
local PRESET = {
  bullet = "BULLET_DISC_CIRCLE_SQUARE",
  ordered = "NUMBERED_DECIMAL_ALPHA_ROMAN",
  task = "BULLET_CHECKBOX",
}

local function len16(s)
  local n = 0
  for _, cp in utf8.codes(s) do
    n = n + (cp >= 0x10000 and 2 or 1)
  end
  return n
end

local function merge(a, b)
  local r = {}
  for k, v in pairs(a) do r[k] = v end
  for k, v in pairs(b or {}) do r[k] = v end
  return r
end

local function field_mask(t)
  local ks = {}
  for k in pairs(t) do ks[#ks + 1] = k end
  table.sort(ks)
  return table.concat(ks, ",")
end

local function rgb(c) return { color = { rgbColor = c } } end
local function pt(m) return { magnitude = m, unit = "PT" } end
local function border(c, width, padding)
  return { color = rgb(c), width = pt(width), padding = pt(padding), dashStyle = "SOLID" }
end

-- Every paragraph property this writer sets, reset to the named style's.
-- Not "*": that includes pageBreakBefore, which Docs refuses inside a table.
local PARAGRAPH_RESET_FIELDS = table.concat({
  "namedStyleType", "alignment", "indentStart", "indentEnd", "indentFirstLine",
  "spaceAbove", "spaceBelow", "shading", "borderLeft", "borderRight",
  "borderTop", "borderBottom", "borderBetween", "lineSpacing",
}, ",")

local MONO = {
  weightedFontFamily = { fontFamily = CODE_FONT },
  fontSize = pt(CODE_SIZE_PT),
}

-- Adds `delta` to every index in a request, leaving alone what the caller
-- places separately (a footnote's content plan, a pending image).
local INDEX_KEYS = { index = true, startIndex = true, endIndex = true }
local function shift(value, delta)
  if type(value) ~= "table" then return value end
  local r = {}
  for k, v in pairs(value) do
    if k == "footnotePlan" or k == "pending" then r[k] = v
    elseif INDEX_KEYS[k] and type(v) == "number" then r[k] = v + delta
    else r[k] = shift(v, delta) end
  end
  return r
end

-- Rendering state for the plan being built; render() saves and restores it
-- around nested plans (table cells, footnotes).
-- Each paragraph: segments = {{text, style} | {op}}, pstyle,
-- list = {run, level} | nil, interior_run = run id | nil.
local paragraphs, runs, current

local function begin(ctx, pstyle, list)
  current = { segments = {}, pstyle = pstyle, list = list, interior_run = ctx.interior_run }
  paragraphs[#paragraphs + 1] = current
end

local function emit(text, style)
  table.insert(current.segments, { text = text, style = style })
end

-- A structural op: `build(index, at_segment_end)` returns its requests,
-- placed at `index`; `at_segment_end` is true when nothing follows the op's
-- paragraph in the segment.
local function emit_op(build, opts)
  table.insert(current.segments, { text = "", style = {}, build = build, table = opts and opts.table })
end

local render

local function pending_image(pending)
  return function(index)
    return { { insertInlineImage = { location = { index = index }, uri = "" }, pending = pending } }
  end
end

local inline
local function inlines(list, style)
  for _, el in ipairs(list) do inline(el, style) end
end

function inline(el, style)
  local t = el.t
  if t == "Str" then emit(el.text, style)
  elseif t == "Space" or t == "SoftBreak" then emit(" ", style)
  elseif t == "LineBreak" then emit(LINE_BREAK, style)
  elseif t == "Emph" then inlines(el.content, merge(style, { italic = true }))
  elseif t == "Strong" then inlines(el.content, merge(style, { bold = true }))
  elseif t == "Strikeout" then inlines(el.content, merge(style, { strikethrough = true }))
  elseif t == "Underline" then inlines(el.content, merge(style, { underline = true }))
  elseif t == "SmallCaps" then inlines(el.content, merge(style, { smallCaps = true }))
  elseif t == "Superscript" then inlines(el.content, merge(style, { baselineOffset = "SUPERSCRIPT" }))
  elseif t == "Subscript" then inlines(el.content, merge(style, { baselineOffset = "SUBSCRIPT" }))
  elseif t == "Code" then
    emit(el.text, merge(style, merge(MONO, { backgroundColor = rgb(CODE_BG) })))
  elseif t == "Link" then
    inlines(el.content, merge(style, {
      link = { url = el.target }, underline = true, foregroundColor = rgb(LINK_FG),
    }))
  elseif t == "Image" then
    local src = el.src
    if src:match("^%a[%w+.-]*:") or src:sub(1, 2) == "//" then
      emit_op(function(index)
        return { { insertInlineImage = { location = { index = index }, uri = src } } }
      end)
    else
      emit_op(pending_image({ kind = "file", path = src }))
    end
  elseif t == "Note" then
    local plan = render(el.content, { quote = 0, indent = 0, list = false, inline = {} })
    emit_op(function(index)
      return { { createFootnote = { location = { index = index } }, footnotePlan = plan } }
    end)
  elseif t == "Quoted" then
    local open, close = "\u{2018}", "\u{2019}"
    if el.quotetype == "DoubleQuote" then open, close = "\u{201C}", "\u{201D}" end
    emit(open, style); inlines(el.content, style); emit(close, style)
  elseif t == "Math" or t == "RawInline" then emit(el.text, style)
  elseif el.content then inlines(el.content, style) -- Span, Cite
  end
end

local function para_style(ctx, extra)
  local s = { namedStyleType = "NORMAL_TEXT" }
  local indent = INDENT_PT * ctx.quote + ctx.indent
  if indent > 0 then
    s.indentStart = pt(indent)
    s.indentFirstLine = pt(indent)
  end
  if ctx.quote > 0 then s.borderLeft = border(QUOTE_BORDER, 3, 8) end
  if ctx.align then s.alignment = ctx.align end
  return merge(s, extra)
end

local block
local function blocks(list, ctx)
  for _, b in ipairs(list) do block(b, ctx) end
end

-- The checkbox state of a task item: TASK_OPEN, TASK_DONE, or nil.
local function task_mark(item)
  local first = item[1]
  if not first or (first.t ~= "Plain" and first.t ~= "Para") then return nil end
  local lead = first.content[1]
  if lead and lead.t == "Str" and (lead.text == TASK_OPEN or lead.text == TASK_DONE) then
    return lead.text
  end
end

local function list_block(b, ctx, kind)
  local is_task = #b.content > 0
  for _, item in ipairs(b.content) do
    if not task_mark(item) then is_task = false end
  end
  local run, level
  if ctx.list then
    run, level = ctx.list.run, ctx.list.level + 1
  else
    runs[#runs + 1] = { preset = PRESET[is_task and "task" or kind] }
    run, level = #runs, 0
  end
  for _, item in ipairs(b.content) do
    local mark = is_task and task_mark(item)
    for i, ib in ipairs(item) do
      if i == 1 and (ib.t == "Plain" or ib.t == "Para") then
        begin(ctx, { namedStyleType = "NORMAL_TEXT" }, { run = run, level = level })
        -- createParagraphBullets reads nesting from leading tabs (and removes them).
        emit(string.rep("\t", level), {})
        local content = ib.content
        local style = ctx.inline
        if mark then
          -- Drop the checkbox glyph and its space. The Docs API cannot set
          -- a checkbox's checked state; a done item is struck through instead.
          content = { table.unpack(content, 3) }
          if mark == TASK_DONE then style = merge(style, { strikethrough = true }) end
        end
        inlines(content, style)
      elseif ib.t == "BulletList" or ib.t == "OrderedList" then
        block(ib, merge(ctx, { list = { run = run, level = level } }))
      else
        block(ib, merge(ctx, {
          list = false, interior_run = run, indent = ctx.indent + INDENT_PT * (level + 1),
        }))
      end
    end
  end
end

-- Requests that insert an empty table at `index` and fill it. Docs puts a
-- newline before the table, so it starts at index + 1; inside, each row and
-- each cell opens with one index, an empty cell holds a lone newline, and the
-- table closes with one more index. Cells are filled last first so the
-- computed indexes of the earlier ones stay valid.
--
-- `index` is normally the newline ending the previous paragraph: the newline
-- Docs inserts then ends that paragraph, and the old one, now an empty
-- paragraph after the table, is deleted (`delete_after`) unless it is the
-- segment's final newline.
local function table_requests(index, rows, ncols, header_rows, delete_after)
  local start = index + 1
  local row_length = 1 + 2 * ncols
  local reqs = { { insertTable = { rows = #rows, columns = ncols, location = { index = index } } } }
  if delete_after then
    local after = start + 2 + #rows * row_length
    reqs[#reqs + 1] = { deleteContentRange = { range = { startIndex = after, endIndex = after + 1 } } }
  end
  for r = #rows, 1, -1 do
    for c = math.min(#rows[r], ncols), 1, -1 do
      local content_index = start + 3 + (r - 1) * row_length + 2 * (c - 1)
      for _, req in ipairs(shift(rows[r][c].requests, content_index - 1)) do
        reqs[#reqs + 1] = req
      end
    end
  end
  if header_rows > 0 then
    reqs[#reqs + 1] = { updateTableCellStyle = {
      tableRange = {
        tableCellLocation = { tableStartLocation = { index = start }, rowIndex = 0, columnIndex = 0 },
        rowSpan = header_rows, columnSpan = ncols,
      },
      tableCellStyle = { backgroundColor = rgb(HEADER_BG) },
      fields = "backgroundColor",
    } }
  end
  return reqs
end

local function table_block(b, ctx)
  if b.caption and b.caption.long and #b.caption.long > 0 then
    blocks(b.caption.long, merge(ctx, { inline = merge(ctx.inline, { italic = true }) }))
  end
  local ncols = #b.colspecs
  local rows, header_rows = {}, 0
  local function add(rs, is_header)
    for _, row in ipairs(rs) do
      local cells = {}
      for c, cell in ipairs(row.cells) do
        cells[#cells + 1] = render(cell.contents, {
          quote = 0, indent = 0, list = false,
          align = ALIGNMENT[cell.alignment] or ALIGNMENT[b.colspecs[c] and b.colspecs[c][1]],
          inline = is_header and { bold = true } or {},
        })
      end
      rows[#rows + 1] = cells
      if is_header then header_rows = header_rows + 1 end
    end
  end
  add(b.head.rows, true)
  for _, body in ipairs(b.bodies) do
    add(body.head, false)
    add(body.body, false)
  end
  add(b.foot.rows, false)
  if #rows == 0 or ncols == 0 then return end
  local build = function(index, at_segment_end)
    return table_requests(index, rows, ncols, header_rows, not at_segment_end)
  end
  if current then
    -- Anchored at the end of the previous paragraph's text, i.e. its newline.
    emit_op(build, { table = true })
  else
    -- First block of the segment: an empty paragraph to anchor at, which
    -- Docs leaves before the table.
    begin(ctx, para_style(ctx))
    emit_op(build, { table = true })
  end
end

function block(b, ctx)
  local t = b.t
  if t == "Para" then
    begin(ctx, para_style(ctx, { spaceBelow = pt(PARA_SPACE_BELOW_PT) }))
    inlines(b.content, ctx.inline)
  elseif t == "Plain" then
    begin(ctx, para_style(ctx))
    inlines(b.content, ctx.inline)
  elseif t == "Header" then
    begin(ctx, { namedStyleType = "HEADING_" .. math.min(b.level, 6) })
    inlines(b.content, ctx.inline)
  elseif t == "CodeBlock" and PLANTUML_CLASSES[b.classes[1] or ""] then
    begin(ctx, para_style(ctx, { spaceBelow = pt(PARA_SPACE_BELOW_PT) }))
    emit_op(pending_image({ kind = "plantuml", source = b.text }))
  elseif t == "CodeBlock" then
    begin(ctx, para_style(ctx, {
      shading = { backgroundColor = rgb(CODE_BG) },
      spaceAbove = pt(4),
      spaceBelow = pt(PARA_SPACE_BELOW_PT),
    }))
    emit((b.text:gsub("\n", LINE_BREAK)), MONO)
  elseif t == "BulletList" then list_block(b, ctx, "bullet")
  elseif t == "OrderedList" then list_block(b, ctx, "ordered")
  elseif t == "BlockQuote" then blocks(b.content, merge(ctx, { quote = ctx.quote + 1, list = false }))
  elseif t == "HorizontalRule" then
    begin(ctx, para_style(ctx, { borderBottom = border(RULE_BORDER, 1, 4) }))
  elseif t == "LineBlock" then
    begin(ctx, para_style(ctx))
    for i, line in ipairs(b.content) do
      if i > 1 then emit(LINE_BREAK, {}) end
      inlines(line, ctx.inline)
    end
  elseif t == "DefinitionList" then
    for _, entry in ipairs(b.content) do
      begin(ctx, para_style(ctx))
      inlines(entry[1], merge(ctx.inline, { bold = true }))
      for _, def in ipairs(entry[2]) do
        blocks(def, merge(ctx, { indent = ctx.indent + INDENT_PT, list = false }))
      end
    end
  elseif t == "Table" then table_block(b, ctx)
  elseif t == "Div" or t == "Figure" then blocks(b.content, ctx)
  end -- RawBlock and anything else: dropped
end

-- The [start, end) range a paragraph-level request should name: its text,
-- or just its newline when it has no text (a range may not be empty).
local function paragraph_range(p, removed)
  removed = removed or 0
  local a, z = p.start - removed, p.finish - removed
  if z > a then return a, z end
  return a, a + 1
end

-- Groups of consecutive paragraphs bulleted together: a list's items plus
-- the non-list blocks between them (bulleted with the rest, then
-- un-bulleted, so the list keeps one id and its numbering continues).
local function bullet_groups()
  local groups, g = {}, nil
  for _, p in ipairs(paragraphs) do
    if p.list and g and p.list.run == g.run then
      g.last_item = p
      table.insert(g.members, p)
    elseif g and p.interior_run == g.run then
      table.insert(g.members, p)
    elseif p.list then
      g = { run = p.list.run, first = p, last_item = p, members = { p } }
      groups[#groups + 1] = g
    else
      g = nil
    end
  end
  return groups
end

-- Leading tabs createParagraphBullets removes before `pos`.
local function tabs_removed_before(pos)
  local n = 0
  for _, p in ipairs(paragraphs) do
    if p.list and p.start < pos then n = n + p.list.level end
  end
  return n
end

local function assemble()
  if #paragraphs == 0 then return { requests = {} } end

  local parts, text_styles, para_styles, ops = {}, {}, {}, {}
  local pos = 1
  for i, p in ipairs(paragraphs) do
    p.start = pos
    for _, s in ipairs(p.segments) do
      if s.build then
        ops[#ops + 1] = { pos = pos, build = s.build, table = s.table, at_segment_end = i == #paragraphs }
      else
        local n = len16(s.text)
        if n > 0 and next(s.style) then
          -- Adjacent runs styled alike (the words and spaces of one Strong)
          -- share one request.
          local key = pandoc.json.encode(s.style)
          local last = text_styles[#text_styles]
          if last and last.key == key and last.updateTextStyle.range.endIndex == pos then
            last.updateTextStyle.range.endIndex = pos + n
          else
            text_styles[#text_styles + 1] = { key = key, updateTextStyle = {
              range = { startIndex = pos, endIndex = pos + n },
              textStyle = s.style, fields = field_mask(s.style),
            } }
          end
        end
        parts[#parts + 1] = s.text
        pos = pos + n
      end
    end
    p.finish = pos
    if i < #paragraphs then parts[#parts + 1] = "\n" end
    pos = pos + 1
    local a, z = paragraph_range(p)
    para_styles[#para_styles + 1] = { updateParagraphStyle = {
      range = { startIndex = a, endIndex = z },
      paragraphStyle = p.pstyle, fields = field_mask(p.pstyle),
    } }
  end

  local whole = { startIndex = 1, endIndex = math.max(2, pos - 1) }
  local requests = {
    { insertText = { location = { index = 1 }, text = table.concat(parts) } },
    -- The inserted text inherits whatever the cleared segment's last
    -- paragraph had; reset it all before styling.
    { deleteParagraphBullets = { range = whole } },
    { updateParagraphStyle = {
      range = whole, paragraphStyle = { namedStyleType = "NORMAL_TEXT" }, fields = PARAGRAPH_RESET_FIELDS,
    } },
    { updateTextStyle = { range = whole, textStyle = {}, fields = "*" } },
  }
  for _, r in ipairs(para_styles) do requests[#requests + 1] = r end
  for _, r in ipairs(text_styles) do
    requests[#requests + 1] = { updateTextStyle = r.updateTextStyle }
  end

  local groups = bullet_groups()
  for gi = #groups, 1, -1 do
    local g = groups[gi]
    local a = paragraph_range(g.first)
    local _, z = paragraph_range(g.last_item)
    requests[#requests + 1] = { createParagraphBullets = {
      range = { startIndex = a, endIndex = z },
      bulletPreset = runs[g.run].preset,
    } }
    -- Interior paragraphs: off the list again, their own style restored.
    -- Earlier groups are untouched so far, so only this group's tabs count.
    local removed = 0
    for _, p in ipairs(g.members) do
      if p.list then
        removed = removed + p.list.level
      elseif p.start < g.last_item.start then
        local pa, pz = paragraph_range(p, removed)
        local range = { startIndex = pa, endIndex = pz }
        requests[#requests + 1] = { deleteParagraphBullets = { range = range } }
        requests[#requests + 1] = { updateParagraphStyle = {
          range = range, paragraphStyle = p.pstyle, fields = field_mask(p.pstyle),
        } }
      end
    end
  end

  for _, op in ipairs(ops) do op.pos = op.pos - tabs_removed_before(op.pos) end
  -- At one position a table runs first: it ends the paragraph there, so an
  -- image or footnote placed at that position afterwards stays before it.
  table.sort(ops, function(x, y)
    if x.pos ~= y.pos then return x.pos > y.pos end
    return x.table and not y.table
  end)
  for _, op in ipairs(ops) do
    for _, r in ipairs(op.build(op.pos, op.at_segment_end)) do requests[#requests + 1] = r end
  end
  return { requests = requests }
end

function render(blocklist, ctx)
  local saved_paragraphs, saved_runs, saved_current = paragraphs, runs, current
  paragraphs, runs, current = {}, {}, nil
  blocks(blocklist, ctx)
  local plan = assemble()
  paragraphs, runs, current = saved_paragraphs, saved_runs, saved_current
  return plan
end

function Writer(doc, opts)
  local plan = render(doc.blocks, { quote = 0, indent = 0, list = false, inline = {} })
  -- Footnote content plans move out of the requests into `footnotes`, in
  -- the order their createFootnote requests run.
  local footnotes = {}
  for _, r in ipairs(plan.requests) do
    if r.footnotePlan then
      footnotes[#footnotes + 1] = r.footnotePlan
      r.footnotePlan = nil
    end
  end
  return pandoc.json.encode({ requests = plan.requests, footnotes = footnotes })
end
